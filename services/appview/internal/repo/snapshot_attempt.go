package repo

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	snapshotJSONLimit  = 65_536
	snapshotBatchLimit = 50
)

// snapshotAttempt holds the per-read request/byte budgets and fetched blocks.
// It is used by a single goroutine.
type snapshotAttempt struct {
	reader    SnapshotReader
	byteCount int
	requests  int
	blocks    map[CID][]byte
}

func newSnapshotAttempt(reader SnapshotReader) *snapshotAttempt {
	return &snapshotAttempt{reader: reader, blocks: map[CID][]byte{}}
}

func (a *snapshotAttempt) read(ctx context.Context, did string, collections map[string]struct{}) (Snapshot, error) {
	base, key, err := a.identity(ctx, did)
	if err != nil {
		return Snapshot{}, err
	}
	if err := a.active(ctx, did, base); err != nil {
		return Snapshot{}, err
	}
	cid, revision, err := a.latest(ctx, did, base)
	if err != nil {
		return Snapshot{}, err
	}
	if err := a.fetchBlocks(ctx, []CID{cid}, did, base); err != nil {
		return Snapshot{}, err
	}
	commit, ok := a.blocks[cid]
	if !ok {
		return Snapshot{}, Invalid("missing commit")
	}
	root, err := ValidateCommit(commit, cid, did, revision, key, a.reader.Now())
	if err != nil {
		return Snapshot{}, err
	}
	if err := a.loadTree(ctx, root, did, base); err != nil {
		return Snapshot{}, err
	}
	tree := NewMST(a.reader.MaximumDepth, a.reader.MaximumBlocks)
	walked, err := tree.Walk(ctx, root, a.blocks)
	if err != nil {
		return Snapshot{}, err
	}
	leaves := filterLeaves(walked, collections)
	if len(leaves) > a.reader.MaximumRecords {
		return Snapshot{}, Invalid("record limit exceeded")
	}
	for start := 0; start < len(leaves); start += snapshotBatchLimit {
		end := min(start+snapshotBatchLimit, len(leaves))
		cids := make([]CID, 0, end-start)
		for _, leaf := range leaves[start:end] {
			cids = append(cids, leaf.CID)
		}
		if err := a.fetchBlocks(ctx, cids, did, base); err != nil {
			return Snapshot{}, err
		}
	}
	records, err := SnapshotRecords(ctx, root, did, collections, a.blocks,
		a.reader.MaximumRecords, a.reader.MaximumDepth, a.reader.MaximumBlocks)
	if err != nil {
		return Snapshot{}, err
	}
	finalBase, finalKey, err := a.identity(ctx, did)
	if err != nil {
		return Snapshot{}, err
	}
	if finalBase != base || !finalKey.Equal(key) {
		return Snapshot{}, Invalid("identity changed")
	}
	if err := a.active(ctx, did, base); err != nil {
		return Snapshot{}, err
	}
	finalCID, finalRevision, err := a.latest(ctx, did, base)
	if err != nil {
		return Snapshot{}, err
	}
	if finalCID != cid || finalRevision != revision {
		return Snapshot{}, Invalid("commit changed")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{PDSBase: base, CommitCID: cid.String(), Revision: revision, Records: records}, nil
}

// get performs a budgeted GET, retrying 429s up to twice (Retry-After capped at 5s).
func (a *snapshotAttempt) get(ctx context.Context, u *url.URL, accept string, limit int) ([]byte, error) {
	for retry := 0; retry <= 2; retry++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.requests++
		if a.requests > a.reader.MaximumRequests {
			return nil, Invalid("request limit exceeded")
		}
		response, err := a.reader.Transport.Get(ctx, u, accept, limit)
		if err != nil {
			return nil, err
		}
		a.byteCount += len(response.Body)
		if len(response.Body) > limit || a.byteCount > a.reader.MaximumBytes {
			return nil, Invalid("byte limit exceeded")
		}
		if response.Status == 429 && retry < 2 {
			delay := 1.0
			if response.RetryAfter != nil {
				if raw, err := strconv.ParseFloat(*response.RetryAfter, 64); err == nil && !math.IsNaN(raw) && !math.IsInf(raw, 0) {
					if raw >= 0 {
						delay = min(raw, 5)
					}
				}
			}
			timer := time.NewTimer(time.Duration(delay * float64(time.Second)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if response.Status != 200 {
			return nil, Invalid("upstream unavailable")
		}
		return response.Body, nil
	}
	return nil, Invalid("upstream unavailable")
}

type queryItem struct{ name, value string }

// query builds base+path with query items in the given order.
func (a *snapshotAttempt) query(base, path string, values []queryItem) (*url.URL, error) {
	u, err := url.Parse(base + path)
	if err != nil {
		return nil, Invalid("invalid URL")
	}
	parts := make([]string, 0, len(values))
	for _, item := range values {
		parts = append(parts, url.QueryEscape(item.name)+"="+url.QueryEscape(item.value))
	}
	u.RawQuery = strings.Join(parts, "&")
	return u, nil
}

func (a *snapshotAttempt) active(ctx context.Context, did, base string) error {
	u, err := a.query(base, "/xrpc/com.atproto.sync.getRepoStatus", []queryItem{{"did", did}})
	if err != nil {
		return err
	}
	data, err := a.get(ctx, u, "application/json", snapshotJSONLimit)
	if err != nil {
		return err
	}
	var status snapshotStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return err
	}
	if status.DID == nil || status.Active == nil {
		return Invalid("invalid repository status")
	}
	if *status.DID != did || !*status.Active {
		return Invalid("inactive repository")
	}
	return nil
}

func (a *snapshotAttempt) latest(ctx context.Context, did, base string) (CID, string, error) {
	u, err := a.query(base, "/xrpc/com.atproto.sync.getLatestCommit", []queryItem{{"did", did}})
	if err != nil {
		return CID{}, "", err
	}
	data, err := a.get(ctx, u, "application/json", snapshotJSONLimit)
	if err != nil {
		return CID{}, "", err
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return CID{}, "", err
	}
	cidText, okCID := object["cid"].(string)
	revision, okRev := object["rev"].(string)
	if object == nil || !okCID || !okRev {
		return CID{}, "", Invalid("invalid latest commit")
	}
	if err := ValidateRevision(revision, a.reader.Now()); err != nil {
		return CID{}, "", err
	}
	commit, err := ParseCID(cidText)
	if err != nil {
		return CID{}, "", err
	}
	if !commit.IsDAGCBOR() {
		return CID{}, "", Invalid("invalid commit codec")
	}
	return commit, revision, nil
}

// identity resolves the DID document and returns the single bound PDS base and #atproto key.
func (a *snapshotAttempt) identity(ctx context.Context, did string) (string, SigningKey, error) {
	var target *url.URL
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		identifier := did[len("did:plc:"):]
		valid := len(identifier) == 24
		for i := 0; valid && i < len(identifier); i++ {
			valid = strings.IndexByte(cidBase32Alphabet, identifier[i]) >= 0
		}
		base := validatedPublicBase(a.reader.PLCBase)
		if !valid || base == "" {
			return "", SigningKey{}, Invalid("invalid DID")
		}
		u, err := url.Parse(base + "/" + did)
		if err != nil {
			return "", SigningKey{}, Invalid("invalid DID")
		}
		target = u
	case strings.HasPrefix(did, "did:web:"):
		parts := strings.Split(did[len("did:web:"):], ":")
		host, err := url.PathUnescape(parts[0])
		if err != nil {
			return "", SigningKey{}, Invalid("invalid web DID")
		}
		base := validatedPublicBase("https://" + host)
		if base == "" {
			return "", SigningKey{}, Invalid("invalid web DID")
		}
		segments := []string{}
		for _, part := range parts[1:] {
			decoded, err := url.PathUnescape(part)
			if err != nil || decoded == "" || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, "/?#\\") {
				return "", SigningKey{}, Invalid("invalid web DID path")
			}
			segments = append(segments, decoded)
		}
		u, err := url.Parse(base)
		if err != nil {
			return "", SigningKey{}, Invalid("invalid web DID")
		}
		if len(segments) == 0 {
			segments = []string{".well-known"}
		}
		u.Path = "/" + strings.Join(append(segments, "did.json"), "/")
		target = u
	default:
		return "", SigningKey{}, Invalid("unsupported DID")
	}
	data, err := a.get(ctx, target, "application/json", snapshotJSONLimit)
	if err != nil {
		return "", SigningKey{}, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return "", SigningKey{}, err
	}
	id, _ := document["id"].(string)
	services, okServices := objectArray(document["service"])
	methods, okMethods := objectArray(document["verificationMethod"])
	if document == nil || id != did || !okServices || !okMethods {
		return "", SigningKey{}, Invalid("identity mismatch")
	}
	var pds []map[string]any
	for _, service := range services {
		if kind, _ := service["type"].(string); kind == "AtprotoPersonalDataServer" {
			pds = append(pds, service)
		}
	}
	if len(pds) != 1 {
		return "", SigningKey{}, Invalid("invalid PDS binding")
	}
	serviceID, _ := pds[0]["id"].(string)
	raw, okRaw := pds[0]["serviceEndpoint"].(string)
	base := ""
	if okRaw {
		base = validatedPublicBase(raw)
	}
	if (serviceID != "#atproto_pds" && serviceID != did+"#atproto_pds") || base == "" {
		return "", SigningKey{}, Invalid("invalid PDS binding")
	}
	var keys []map[string]any
	for _, method := range methods {
		if methodID, _ := method["id"].(string); methodID == "#atproto" || methodID == did+"#atproto" {
			keys = append(keys, method)
		}
	}
	if len(keys) != 1 {
		return "", SigningKey{}, Invalid("invalid signing key binding")
	}
	controller, _ := keys[0]["controller"].(string)
	kind, _ := keys[0]["type"].(string)
	multibase, okKey := keys[0]["publicKeyMultibase"].(string)
	if controller != did || kind != "Multikey" || !okKey {
		return "", SigningKey{}, Invalid("invalid signing key binding")
	}
	key, err := ParseSigningKey(multibase)
	if err != nil {
		return "", SigningKey{}, err
	}
	return base, key, nil
}

// objectArray asserts value is a JSON array whose every element is an object.
func objectArray(value any) ([]map[string]any, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, object)
	}
	return out, true
}

func (a *snapshotAttempt) fetchBlocks(ctx context.Context, cids []CID, did, base string) error {
	for _, cid := range cids {
		if !cid.IsDAGCBOR() {
			return Invalid("invalid block CID codec")
		}
	}
	requested := map[CID]struct{}{}
	for _, cid := range cids {
		if _, have := a.blocks[cid]; !have {
			requested[cid] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	names := make([]string, 0, len(requested))
	for cid := range requested {
		names = append(names, cid.String())
	}
	slices.Sort(names)
	values := []queryItem{{"did", did}}
	for _, name := range names {
		values = append(values, queryItem{"cids", name})
	}
	u, err := a.query(base, "/xrpc/com.atproto.sync.getBlocks", values)
	if err != nil {
		return err
	}
	data, err := a.get(ctx, u, "application/vnd.ipld.car", maximumCARBytes)
	if err != nil {
		return err
	}
	fetched, err := CARBlocks(ctx, data, requested, a.reader.MaximumBlocks-len(a.blocks))
	if err != nil {
		return err
	}
	for cid, raw := range fetched {
		a.blocks[cid] = raw
	}
	return nil
}

// loadTree fetches every MST node breadth-first in batches of 50.
func (a *snapshotAttempt) loadTree(ctx context.Context, root CID, did, base string) error {
	type item struct {
		cid   CID
		depth int
	}
	queue := []item{{root, 0}}
	seen := map[CID]struct{}{root: {}}
	offset := 0
	for offset < len(queue) {
		end := min(offset+snapshotBatchLimit, len(queue))
		batch := append([]item(nil), queue[offset:end]...)
		offset = end
		cids := make([]CID, 0, len(batch))
		for _, entry := range batch {
			cids = append(cids, entry.cid)
		}
		if err := a.fetchBlocks(ctx, cids, did, base); err != nil {
			return err
		}
		for _, entry := range batch {
			raw, ok := a.blocks[entry.cid]
			if !ok {
				return Invalid("missing tree block")
			}
			node, err := DecodeDAGCBOR(raw)
			if err != nil {
				return err
			}
			entriesValue, _ := node.Field("e")
			entries, ok := entriesValue.Values()
			if !ok {
				return Invalid("invalid tree node")
			}
			left, err := mstChild(node.Field("l"))
			if err != nil {
				return err
			}
			children := []*CID{left}
			for _, child := range entries {
				right, err := mstChild(child.Field("t"))
				if err != nil {
					return err
				}
				children = append(children, right)
			}
			for _, child := range children {
				if child == nil {
					continue
				}
				_, repeated := seen[*child]
				if entry.depth >= a.reader.MaximumDepth || repeated || len(seen)+1 > a.reader.MaximumBlocks {
					return Invalid("repeated or excessive MST reference")
				}
				seen[*child] = struct{}{}
				queue = append(queue, item{*child, entry.depth + 1})
			}
		}
	}
	return nil
}
