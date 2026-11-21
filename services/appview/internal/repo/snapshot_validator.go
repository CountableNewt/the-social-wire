package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
)

const tidAlphabet = "234567abcdefghijklmnopqrstuvwxyz"

// ValidateCommit verifies a signed v3 commit block and returns its MST root CID.
func ValidateCommit(raw []byte, cid CID, did, revision string, key SigningKey, now time.Time) (CID, error) {
	if err := cid.Verify(raw); err != nil {
		return CID{}, err
	}
	if !cid.IsDAGCBOR() {
		return CID{}, Invalid("invalid commit")
	}
	value, err := DecodeDAGCBOR(raw)
	if err != nil {
		return CID{}, err
	}
	invalid := Invalid("invalid commit")
	if value.Kind != KindMap || !hasExactKeys(value.Map, "did", "version", "rev", "prev", "data", "sig") {
		return CID{}, invalid
	}
	fields := value.Map
	if text, ok := fields["did"].Text(); !ok || text != did {
		return CID{}, invalid
	}
	if version, ok := fields["version"].Number(); !ok || version != 3 {
		return CID{}, invalid
	}
	if rev, ok := fields["rev"].Text(); !ok || rev != revision {
		return CID{}, invalid
	}
	root, ok := fields["data"].CID()
	if !ok || !root.IsDAGCBOR() {
		return CID{}, invalid
	}
	signature, ok := fields["sig"].Data()
	if !ok {
		return CID{}, invalid
	}
	if _, err := mstChild(fields["prev"], true); err != nil {
		return CID{}, err
	}
	if err := ValidateRevision(revision, now); err != nil {
		return CID{}, err
	}
	unsigned := make(map[string]Value, len(fields)-1)
	for name, field := range fields {
		if name != "sig" {
			unsigned[name] = field
		}
	}
	if err := key.Verify(signature, EncodeDAGCBOR(MapValue(unsigned))); err != nil {
		return CID{}, err
	}
	return root, nil
}

// SnapshotRecords walks the verified tree and returns the records in the
// requested collections with values converted to sorted-key JSON.
func SnapshotRecords(ctx context.Context, root CID, did string, collections map[string]struct{}, blocks map[CID][]byte, maximumRecords, maximumDepth, maximumBlocks int) ([]SnapshotRecord, error) {
	tree := NewMST(maximumDepth, maximumBlocks)
	leaves, err := tree.Walk(ctx, root, blocks)
	if err != nil {
		return nil, err
	}
	relevant := filterLeaves(leaves, collections)
	if len(relevant) > maximumRecords {
		return nil, Invalid("record limit exceeded")
	}
	records := make([]SnapshotRecord, 0, len(relevant))
	for _, leaf := range relevant {
		data, ok := blocks[leaf.CID]
		if !ok {
			return nil, Invalid("missing record block")
		}
		if err := leaf.CID.Verify(data); err != nil {
			return nil, err
		}
		value, err := DecodeDAGCBOR(data)
		if err != nil {
			return nil, err
		}
		if value.Kind != KindMap {
			return nil, Invalid("invalid record object")
		}
		encoded, err := marshalSortedJSON(value.JSONObject())
		if err != nil {
			return nil, err
		}
		records = append(records, SnapshotRecord{URI: "at://" + did + "/" + leaf.Path, CID: leaf.CID.String(), Value: encoded})
	}
	return records, nil
}

// filterLeaves keeps leaves whose collection (path before the first "/") is requested.
func filterLeaves(leaves []MSTLeaf, collections map[string]struct{}) []MSTLeaf {
	out := []MSTLeaf{}
	for _, leaf := range leaves {
		collection, _, _ := strings.Cut(leaf.Path, "/")
		if _, ok := collections[collection]; ok {
			out = append(out, leaf)
		}
	}
	return out
}

// marshalSortedJSON encodes with sorted map keys and without HTML escaping.
func marshalSortedJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// ValidateRevision checks a 13-character TID revision is well formed and not
// more than five minutes in the future.
func ValidateRevision(revision string, now time.Time) error {
	if len(revision) != 13 {
		return Invalid("invalid revision")
	}
	var value uint64
	for i := 0; i < len(revision); i++ {
		digit := strings.IndexByte(tidAlphabet, revision[i])
		if digit < 0 || value > ^uint64(0)>>5 {
			return Invalid("invalid revision")
		}
		value = value<<5 | uint64(digit)
	}
	nowSeconds := float64(now.UnixNano()) / 1e9
	if float64(value>>10)/1_000_000 > nowSeconds+300 {
		return Invalid("future revision")
	}
	return nil
}
