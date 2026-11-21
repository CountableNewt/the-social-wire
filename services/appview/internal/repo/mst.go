package repo

import (
	"context"
	"crypto/sha256"
	"math/bits"
	"strings"
	"unicode/utf8"
)

// MSTLeaf is one record path and its record CID in a repository tree.
type MSTLeaf struct {
	Path string
	CID  CID
}

// MST walks a repository Merkle Search Tree with depth and size bounds and
// verifies that the walked leaves reproduce the canonical root.
type MST struct {
	leaves         []MSTLeaf
	visited        map[CID]struct{}
	maximumDepth   int
	maximumRecords int
}

// NewMST returns a walker; Swift defaults are maximumDepth 64, maximumRecords 100_000.
func NewMST(maximumDepth, maximumRecords int) *MST {
	return &MST{visited: map[CID]struct{}{}, maximumDepth: maximumDepth, maximumRecords: maximumRecords}
}

// DefaultMST returns a walker with the Swift default bounds.
func DefaultMST() *MST { return NewMST(64, 100_000) }

// Leaves returns the leaves collected so far.
func (t *MST) Leaves() []MSTLeaf { return t.leaves }

// Walk traverses the tree rooted at root using blocks and returns its leaves in order.
func (t *MST) Walk(ctx context.Context, root CID, blocks map[CID][]byte) ([]MSTLeaf, error) {
	if err := t.walk(ctx, root, 0, blocks); err != nil {
		return nil, err
	}
	canonical, err := CanonicalMSTRoot(t.leaves)
	if err != nil {
		return nil, err
	}
	if canonical != root {
		return nil, Invalid("noncanonical MST")
	}
	return t.leaves, nil
}

func (t *MST) walk(ctx context.Context, cid CID, depth int, blocks map[CID][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, seen := t.visited[cid]
	if !seen {
		t.visited[cid] = struct{}{}
	}
	data, present := blocks[cid]
	if depth > t.maximumDepth || seen || len(t.visited) > t.maximumRecords || !present {
		return Invalid("invalid or incomplete MST")
	}
	if err := cid.Verify(data); err != nil {
		return err
	}
	node, err := DecodeDAGCBOR(data)
	if err != nil {
		return err
	}
	entriesValue, _ := node.Field("e")
	entries, isArray := entriesValue.Values()
	if node.Kind != KindMap || !hasExactKeys(node.Map, "e", "l") || !isArray {
		return Invalid("invalid MST node")
	}
	left, err := mstChild(node.Field("l"))
	if err != nil {
		return err
	}
	if left != nil {
		if err := t.walk(ctx, *left, depth+1, blocks); err != nil {
			return err
		}
	}
	var previous []byte
	for _, entry := range entries {
		prefixValue, _ := entry.Field("p")
		suffixValue, _ := entry.Field("k")
		valueValue, _ := entry.Field("v")
		prefix, isNumber := prefixValue.Number()
		suffix, isBytes := suffixValue.Data()
		value, isLink := valueValue.CID()
		if entry.Kind != KindMap || !hasExactKeys(entry.Map, "p", "k", "v", "t") ||
			!isNumber || prefix > uint64(len(previous)) || !isBytes || !isLink || !value.IsDAGCBOR() {
			return Invalid("invalid MST entry")
		}
		pathBytes := append(append([]byte(nil), previous[:prefix]...), suffix...)
		path := string(pathBytes)
		if !utf8.Valid(pathBytes) || !ValidRecordPath(path) ||
			(len(t.leaves) > 0 && !(t.leaves[len(t.leaves)-1].Path < path)) {
			return Invalid("invalid MST ordering")
		}
		t.leaves = append(t.leaves, MSTLeaf{Path: path, CID: value})
		if len(t.leaves) > t.maximumRecords {
			return Invalid("MST record limit")
		}
		previous = pathBytes
		right, err := mstChild(entry.Field("t"))
		if err != nil {
			return err
		}
		if right != nil {
			if err := t.walk(ctx, *right, depth+1, blocks); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasExactKeys(m map[string]Value, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			return false
		}
	}
	return true
}

// mstChild interprets an optional subtree pointer: null means no child; a
// missing field or anything other than a DAG-CBOR link is invalid.
func mstChild(value Value, present bool) (*CID, error) {
	if present && value.Kind == KindNull {
		return nil, nil
	}
	cid, isLink := value.CID()
	if !present || !isLink || !cid.IsDAGCBOR() {
		return nil, Invalid("invalid MST child")
	}
	return &cid, nil
}

// ValidRecordPath validates "<nsid>/<rkey>" repository paths.
func ValidRecordPath(path string) bool {
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return false
	}
	rkey := parts[1]
	if len(rkey) > 512 || rkey == "" || rkey == "." || rkey == ".." {
		return false
	}
	for i := 0; i < len(rkey); i++ {
		b := rkey[i]
		if !(isASCIIDigit(b) || isASCIIUpper(b) || isASCIILower(b) || b == '.' || b == '_' || b == '~' || b == ':' || b == '-') {
			return false
		}
	}
	labels := strings.Split(parts[0], ".")
	if len(labels) < 3 || len(parts[0]) > 317 {
		return false
	}
	for index, label := range labels {
		if label == "" || len(label) > 63 {
			return false
		}
		first, last := label[0], label[len(label)-1]
		middle := index > 0 && index < len(labels)-1
		if !(isASCIIUpper(first) || isASCIILower(first) || (middle && isASCIIDigit(first))) || last == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			b := label[i]
			if !(isASCIIUpper(b) || isASCIILower(b) || isASCIIDigit(b) || (index < len(labels)-1 && b == '-')) {
				return false
			}
		}
	}
	return true
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }
func isASCIIUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isASCIILower(b byte) bool { return b >= 'a' && b <= 'z' }

// MSTHeight is the tree layer of a key: leading zero bits of SHA-256(key) / 2.
func MSTHeight(path string) int {
	sum := sha256.Sum256([]byte(path))
	zeros := 0
	for _, b := range sum {
		if b == 0 {
			zeros += 8
			continue
		}
		zeros += bits.LeadingZeros8(b)
		break
	}
	return zeros / 2
}

// CanonicalMSTRoot rebuilds the canonical tree for sorted leaves and returns its root CID.
func CanonicalMSTRoot(leaves []MSTLeaf) (CID, error) {
	heights := make([]int, len(leaves))
	maxHeight := 0
	for i, leaf := range leaves {
		heights[i] = MSTHeight(leaf.Path)
		if heights[i] > maxHeight {
			maxHeight = heights[i]
		}
	}
	return mstNode(leaves, heights, maxHeight)
}

func mstNode(leaves []MSTLeaf, heights []int, height int) (CID, error) {
	if height < 0 {
		return CID{}, Invalid("invalid MST height")
	}
	var positions []int
	for i, h := range heights {
		if h == height {
			positions = append(positions, i)
		}
	}
	if len(positions) == 0 {
		left := NullValue()
		if len(leaves) > 0 {
			child, err := mstNode(leaves, heights, height-1)
			if err != nil {
				return CID{}, err
			}
			left = LinkValue(child)
		}
		return HashCID(EncodeDAGCBOR(MapValue(map[string]Value{"e": ArrayValue([]Value{}), "l": left}))), nil
	}
	entries := make([]Value, 0, len(positions))
	var previous []byte
	first := positions[0]
	left := NullValue()
	if first != 0 {
		child, err := mstNode(leaves[:first], heights[:first], height-1)
		if err != nil {
			return CID{}, err
		}
		left = LinkValue(child)
	}
	for offset, index := range positions {
		leaf := leaves[index]
		key := []byte(leaf.Path)
		next := len(leaves)
		if offset+1 < len(positions) {
			next = positions[offset+1]
		}
		prefix := commonPrefixLength(previous, key)
		right := NullValue()
		if index+1 != next {
			child, err := mstNode(leaves[index+1:next], heights[index+1:next], height-1)
			if err != nil {
				return CID{}, err
			}
			right = LinkValue(child)
		}
		entries = append(entries, MapValue(map[string]Value{
			"p": UnsignedValue(uint64(prefix)),
			"k": BytesValue(key[prefix:]),
			"v": LinkValue(leaf.CID),
			"t": right,
		}))
		previous = key
	}
	return HashCID(EncodeDAGCBOR(MapValue(map[string]Value{"e": ArrayValue(entries), "l": left}))), nil
}

func commonPrefixLength(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
