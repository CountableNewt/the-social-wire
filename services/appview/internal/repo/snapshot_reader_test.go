package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var documentCollections = []string{"site.standard.document"}

func TestVerifiesIndependentSignedFixtures(t *testing.T) {
	for _, tc := range []struct {
		curve   string
		records int
	}{{"p256", 63}, {"k256", 63}, {"p256-large", 250}} {
		t.Run(tc.curve, func(t *testing.T) {
			fixture := loadFixture(t, tc.curve)
			transport := newFixtureTransport(t, fixture, mutationNone)
			snapshot, err := NewSnapshotReader(transport, "").Read(context.Background(), fixture.did, documentCollections)
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.CommitCID != fixture.cid.String() || snapshot.PDSBase != fixture.pds || snapshot.Revision != fixture.revision {
				t.Fatalf("unexpected snapshot header %+v", snapshot)
			}
			if len(snapshot.Records) != tc.records {
				t.Fatalf("records = %d", len(snapshot.Records))
			}
			if got := snapshot.Records[0].URI; got != "at://"+fixture.did+"/site.standard.document/key000" {
				t.Fatalf("first uri = %s", got)
			}
			for _, record := range snapshot.Records {
				if !strings.Contains(record.URI, "/site.standard.document/") {
					t.Fatalf("unexpected record %s", record.URI)
				}
			}
			var value map[string]any
			if err := json.Unmarshal(snapshot.Records[0].Value, &value); err != nil {
				t.Fatal(err)
			}
			if value["title"] != "Record 0" {
				t.Fatalf("title = %v", value["title"])
			}
			if b, _ := value["bytes"].(map[string]any); len(b) != 1 || b["$bytes"] != "AQI" {
				t.Fatalf("bytes = %v", value["bytes"])
			}
			blob, _ := value["blob"].(map[string]any)
			ref, _ := blob["ref"].(map[string]any)
			if link, _ := ref["$link"].(string); !strings.HasPrefix(link, "bafk") {
				t.Fatalf("blob ref = %v", ref)
			}
			if n := transport.count("/xrpc/com.atproto.sync.getLatestCommit"); n != 2 {
				t.Fatalf("getLatestCommit calls = %d", n)
			}
		})
	}
}

func TestRejectsUnsafeOrIncompleteSnapshots(t *testing.T) {
	fixture := loadFixture(t, "p256")
	for name, m := range map[string]mutation{
		"inactive": mutationInactive, "inactiveAtEnd": mutationInactiveAtEnd,
		"numericActive": mutationNumericActive, "identityChanged": mutationIdentityChanged,
		"signingKeyChanged": mutationSigningKeyChanged, "commitChanged": mutationCommitChanged,
		"missingBlock": mutationMissingBlock, "corruptBlock": mutationCorruptBlock,
		"unrequestedBlock": mutationUnrequestedBlock,
	} {
		t.Run(name, func(t *testing.T) {
			transport := newFixtureTransport(t, fixture, m)
			if _, err := NewSnapshotReader(transport, "").Read(context.Background(), fixture.did, documentCollections); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func commitFields(t *testing.T, fixture snapshotFixture) map[string]Value {
	t.Helper()
	commit, err := DecodeDAGCBOR(fixture.blocks[fixture.cid])
	if err != nil || commit.Kind != KindMap {
		t.Fatalf("missing fixture commit: %v", err)
	}
	return commit.Map
}

func TestRejectsCorruptSignaturesEvenWithValidBlockHash(t *testing.T) {
	for _, curve := range []string{"p256", "k256"} {
		t.Run(curve, func(t *testing.T) {
			fixture := loadFixture(t, curve)
			fields := commitFields(t, fixture)
			signature := append([]byte(nil), fields["sig"].Bytes...)
			signature[0] ^= 1
			fields["sig"] = BytesValue(signature)
			corrupt := EncodeDAGCBOR(MapValue(fields))
			key, err := ParseSigningKey(fixture.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateCommit(corrupt, HashCID(corrupt), fixture.did, fixture.revision, key, time.Now()); err == nil {
				t.Fatal("expected error")
			}
			// The untouched commit verifies.
			raw := fixture.blocks[fixture.cid]
			if _, err := ValidateCommit(raw, fixture.cid, fixture.did, fixture.revision, key, time.Now()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRejectsHighSSignatures(t *testing.T) {
	p256Order := []byte{
		0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xbc, 0xe6, 0xfa, 0xad, 0xa7, 0x17, 0x9e, 0x84, 0xf3, 0xb9, 0xca, 0xc2, 0xfc, 0x63, 0x25, 0x51,
	}
	k256Order := []byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfe,
		0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b, 0xbf, 0xd2, 0x5e, 0x8c, 0xd0, 0x36, 0x41, 0x41,
	}
	for _, curve := range []string{"p256", "k256"} {
		t.Run(curve, func(t *testing.T) {
			fixture := loadFixture(t, curve)
			fields := commitFields(t, fixture)
			signature := fields["sig"].Bytes
			order := k256Order
			want := "invalid commit signature"
			if curve == "p256" {
				order, want = p256Order, "high-S signature"
			}
			s := signature[32:]
			highS := make([]byte, 32)
			borrow := 0
			for i := 31; i >= 0; i-- {
				value := int(order[i]) - int(s[i]) - borrow
				borrow = 0
				if value < 0 {
					value += 256
					borrow = 1
				}
				highS[i] = byte(value)
			}
			fields["sig"] = BytesValue(append(append([]byte(nil), signature[:32]...), highS...))
			raw := EncodeDAGCBOR(MapValue(fields))
			key, err := ParseSigningKey(fixture.key)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ValidateCommit(raw, HashCID(raw), fixture.did, fixture.revision, key, time.Now())
			if !errors.Is(err, Invalid(want)) {
				t.Fatalf("err = %v, want %s", err, want)
			}
		})
	}
}

func TestRejectsSharedMSTChildrenAndWholeSetPrivateDNS(t *testing.T) {
	fixture := loadFixture(t, "p256")
	root, _ := commitFields(t, fixture)["data"].CID()
	node, err := DecodeDAGCBOR(fixture.blocks[root])
	if err != nil {
		t.Fatal(err)
	}
	entries := node.Map["e"].Array
	if len(entries) == 0 {
		t.Fatal("missing fixture MST")
	}
	first := entries[0].Map
	child, ok := node.Map["l"].CID()
	if !ok {
		child, ok = first["t"].CID()
	}
	if !ok {
		t.Fatal("fixture root has no child")
	}
	node.Map["l"] = LinkValue(child)
	first["t"] = LinkValue(child)
	raw := EncodeDAGCBOR(node)
	altered := HashCID(raw)
	blocks := map[CID][]byte{altered: raw}
	for cid, data := range fixture.blocks {
		blocks[cid] = data
	}
	if _, err := DefaultMST().Walk(context.Background(), altered, blocks); err == nil {
		t.Fatal("expected shared child rejection")
	}
	for _, tc := range []struct {
		in   []string
		want string
	}{
		{[]string{"8.8.8.8", "127.0.0.1"}, ""},
		{[]string{"8.8.8.8", "10.0.0.1"}, ""},
		{[]string{"8.8.8.8", "::ffff:127.0.0.1"}, ""},
		{[]string{"2606:4700:4700::1111", "8.8.8.8"}, "8.8.8.8"},
	} {
		got, _ := PreferredPublicAddress(tc.in)
		if got != tc.want {
			t.Errorf("PreferredPublicAddress(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBoundsWholeAttemptIncludingRetries(t *testing.T) {
	fixture := loadFixture(t, "p256")
	rateLimited := newFixtureTransport(t, fixture, mutationRateLimited)
	if _, err := NewSnapshotReader(rateLimited, "").Read(context.Background(), fixture.did, documentCollections); err == nil {
		t.Fatal("expected rate-limit failure")
	}
	if n := rateLimited.count("/" + fixture.did); n != 3 {
		t.Fatalf("identity calls = %d", n)
	}
	byteLimited := NewSnapshotReader(newFixtureTransport(t, fixture, mutationNone), "")
	byteLimited.MaximumBytes = 8
	if _, err := byteLimited.Read(context.Background(), fixture.did, documentCollections); err == nil {
		t.Fatal("expected byte limit failure")
	}
	requestLimited := NewSnapshotReader(newFixtureTransport(t, fixture, mutationNone), "")
	requestLimited.MaximumRequests = 1
	if _, err := requestLimited.Read(context.Background(), fixture.did, documentCollections); err == nil {
		t.Fatal("expected request limit failure")
	}
}

func TestCancelsAnUnresponsiveTransport(t *testing.T) {
	fixture := loadFixture(t, "p256")
	transport := newFixtureTransport(t, fixture, mutationStall)
	reader := NewSnapshotReader(transport, "")
	reader.Timeout = 5 * time.Millisecond
	_, err := reader.Read(context.Background(), fixture.did, documentCollections)
	if !errors.Is(err, Invalid("snapshot deadline exceeded")) {
		t.Fatalf("err = %v", err)
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.active != 0 || transport.cancelled != 1 {
		t.Fatalf("active = %d cancelled = %d", transport.active, transport.cancelled)
	}
}

func TestRejectsInvalidCollections(t *testing.T) {
	reader := NewSnapshotReader(newFixtureTransport(t, loadFixture(t, "p256"), mutationNone), "")
	for _, collections := range [][]string{nil, {"bad"}} {
		if _, err := reader.Read(context.Background(), "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa", collections); !errors.Is(err, Invalid("invalid collection")) {
			t.Fatalf("collections %v err = %v", collections, err)
		}
	}
}

func TestPreservesValidNumericNSIDDomainLabels(t *testing.T) {
	for path, want := range map[string]bool{
		"com.123.record/key":        true,
		"com.a-123.record/key":      true,
		"123.example.record/key":    false,
		"com.example.123record/key": false,
		"com.-123.record/key":       false,
		"com.123-.record/key":       false,
	} {
		if got := ValidRecordPath(path); got != want {
			t.Errorf("ValidRecordPath(%q) = %v", path, got)
		}
	}
}

func TestRejectsNoncanonicalCBORAndCIDEncoding(t *testing.T) {
	for _, data := range [][]byte{
		{0x18, 0x01},
		{0xa2, 0x61, 0x62, 0x01, 0x61, 0x61, 0x02},
		{0xa2, 0x61, 0x61, 0x01, 0x61, 0x61, 0x02},
	} {
		if _, err := DecodeDAGCBOR(data); err == nil {
			t.Errorf("DecodeDAGCBOR(%x) accepted", data)
		}
	}
	if _, err := ParseCID("b" + strings.Repeat("a", 58)); err == nil {
		t.Error("ParseCID accepted invalid CID")
	}
	if err := ValidateRevision("zzzzzzzzzzzzz", time.Now()); err == nil {
		t.Error("ValidateRevision accepted overflow")
	}
}

func TestDAGCBORRoundTripsFixtureBlocks(t *testing.T) {
	fixture := loadFixture(t, "k256")
	for cid, data := range fixture.blocks {
		value, err := DecodeDAGCBOR(data)
		if err != nil {
			t.Fatalf("%s: %v", cid, err)
		}
		if !bytes.Equal(EncodeDAGCBOR(value), data) || HashCID(data) != cid {
			t.Fatalf("%s did not round-trip", cid)
		}
		parsed, err := ParseCID(cid.String())
		if err != nil || parsed != cid {
			t.Fatalf("CID string round trip failed for %s", cid)
		}
	}
}
