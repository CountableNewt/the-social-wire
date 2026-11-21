package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// snapshotFixture was generated with the independent github.com/jcalabro/atmos
// v0.3.7 repository writer: 63 document records (250 in p256-large), an
// unrelated post, a signed commit, and canonical MST blocks.
type snapshotFixture struct {
	did, pds, key, revision string
	cid                     CID
	document                []byte
	blocks                  map[CID][]byte
}

func loadFixture(t *testing.T, name string) snapshotFixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		DID      string            `json:"did"`
		PDS      string            `json:"pds"`
		Key      string            `json:"key"`
		Revision string            `json:"revision"`
		CID      string            `json:"cid"`
		Document json.RawMessage   `json:"document"`
		Blocks   map[string]string `json:"blocks"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	cid, err := ParseCID(value.CID)
	if err != nil {
		t.Fatal(err)
	}
	blocks := map[CID][]byte{}
	for key, encoded := range value.Blocks {
		blockCID, err := ParseCID(key)
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		blocks[blockCID] = data
	}
	return snapshotFixture{did: value.DID, pds: value.PDS, key: value.Key, revision: value.Revision,
		cid: cid, document: value.Document, blocks: blocks}
}

type mutation int

const (
	mutationNone mutation = iota
	mutationInactive
	mutationInactiveAtEnd
	mutationNumericActive
	mutationIdentityChanged
	mutationSigningKeyChanged
	mutationCommitChanged
	mutationMissingBlock
	mutationCorruptBlock
	mutationUnrequestedBlock
	mutationRateLimited
	mutationStall
)

type fixtureTransport struct {
	t        *testing.T
	fixture  snapshotFixture
	mutation mutation

	mu        sync.Mutex
	calls     map[string]int
	active    int
	cancelled int
}

func newFixtureTransport(t *testing.T, fixture snapshotFixture, m mutation) *fixtureTransport {
	return &fixtureTransport{t: t, fixture: fixture, mutation: m, calls: map[string]int{}}
}

func (f *fixtureTransport) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *fixtureTransport) Get(ctx context.Context, u *url.URL, accept string, maximumBytes int) (HTTPResponse, error) {
	f.mu.Lock()
	f.active++
	f.calls[u.Path]++
	call := f.calls[u.Path]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		if ctx.Err() != nil {
			f.cancelled++
		}
		f.mu.Unlock()
	}()
	if f.mutation == mutationStall {
		select {
		case <-ctx.Done():
			return HTTPResponse{}, ctx.Err()
		case <-time.After(time.Hour):
		}
	}
	if f.mutation == mutationRateLimited {
		zero := "0"
		return HTTPResponse{Status: 429, RetryAfter: &zero}, nil
	}
	var body []byte
	switch u.Path {
	case "/" + f.fixture.did:
		var document map[string]any
		_ = json.Unmarshal(f.fixture.document, &document)
		switch {
		case f.mutation == mutationIdentityChanged && call == 2:
			document["service"] = []any{map[string]any{
				"id": "#atproto_pds", "type": "AtprotoPersonalDataServer",
				"serviceEndpoint": "https://moved.publisher.social",
			}}
		case f.mutation == mutationSigningKeyChanged && call == 2:
			methods := document["verificationMethod"].([]any)
			methods[0].(map[string]any)["publicKeyMultibase"] = loadFixture(f.t, "k256").key
		}
		body, _ = json.Marshal(document)
	case "/xrpc/com.atproto.sync.getRepoStatus":
		var active any = f.mutation != mutationInactive && !(f.mutation == mutationInactiveAtEnd && call == 2)
		if f.mutation == mutationNumericActive {
			active = 1
		}
		body, _ = json.Marshal(map[string]any{"did": f.fixture.did, "active": active})
	case "/xrpc/com.atproto.sync.getLatestCommit":
		revision := f.fixture.revision
		if f.mutation == mutationCommitChanged && call == 2 {
			last := "2"
			if revision[len(revision)-1] == '2' {
				last = "3"
			}
			revision = revision[:len(revision)-1] + last
		}
		body, _ = json.Marshal(map[string]any{"cid": f.fixture.cid.String(), "rev": revision})
	case "/xrpc/com.atproto.sync.getBlocks":
		var requested []CID
		for _, text := range u.Query()["cids"] {
			cid, err := ParseCID(text)
			if err != nil {
				return HTTPResponse{}, err
			}
			requested = append(requested, cid)
		}
		if f.mutation == mutationMissingBlock {
			requested = requested[:len(requested)-1]
		}
		if f.mutation == mutationUnrequestedBlock {
			for cid := range f.fixture.blocks {
				found := false
				for _, r := range requested {
					found = found || r == cid
				}
				if !found {
					requested = append(requested, cid)
					break
				}
			}
		}
		car := carSection(EncodeDAGCBOR(MapValue(map[string]Value{
			"version": UnsignedValue(1), "roots": ArrayValue([]Value{}),
		})))
		for _, cid := range requested {
			block := append([]byte(nil), f.fixture.blocks[cid]...)
			if f.mutation == mutationCorruptBlock {
				block[0] ^= 1
			}
			car = append(car, carSection(append(cid.Bytes(), block...))...)
		}
		body = car
	default:
		return HTTPResponse{}, errors.New("unexpected fixture request")
	}
	return HTTPResponse{Status: 200, Body: body}, nil
}

func carSection(data []byte) []byte {
	length := len(data)
	var prefix []byte
	for {
		next := length & 127
		length >>= 7
		if length == 0 {
			prefix = append(prefix, byte(next))
			break
		}
		prefix = append(prefix, byte(next|128))
	}
	return append(prefix, data...)
}
