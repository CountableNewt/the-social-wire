package repo

import "context"

// maximumCARBytes bounds a single getBlocks CAR response.
const maximumCARBytes = 8 * 1024 * 1024

// CARBlocks parses a CARv1 archive and returns exactly the requested DAG-CBOR
// blocks, each verified against its CID. Unrequested, duplicate, missing or
// excess blocks are rejected.
func CARBlocks(ctx context.Context, data []byte, requested map[CID]struct{}, maximumBlocks int) (map[CID][]byte, error) {
	if len(data) == 0 || len(data) > maximumCARBytes {
		return nil, Invalid("CAR size limit exceeded")
	}
	cursor := newBinaryCursor(data)
	headerLength, err := cursor.varint()
	if err != nil {
		return nil, err
	}
	headerBytes, err := cursor.read(headerLength)
	if err != nil {
		return nil, err
	}
	header, err := DecodeDAGCBOR(headerBytes)
	if err != nil {
		return nil, err
	}
	version, _ := header.Field("version")
	rootsValue, _ := header.Field("roots")
	number, isNumber := version.Number()
	roots, isArray := rootsValue.Values()
	if !isNumber || number != 1 || !isArray {
		return nil, Invalid("invalid CAR header")
	}
	for _, root := range roots {
		if root.Kind != KindLink {
			return nil, Invalid("invalid CAR header")
		}
	}
	remaining := make(map[CID]struct{}, len(requested))
	for cid := range requested {
		remaining[cid] = struct{}{}
	}
	blocks := map[CID][]byte{}
	for cursor.remaining() > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		length, err := cursor.varint()
		if err != nil {
			return nil, err
		}
		if length < 36 {
			return nil, Invalid("invalid CAR section")
		}
		sectionBytes, err := cursor.read(length)
		if err != nil {
			return nil, err
		}
		cid, err := ParseCIDBytes(sectionBytes[:36])
		if err != nil {
			return nil, err
		}
		if !cid.IsDAGCBOR() {
			return nil, Invalid("invalid CAR block codec")
		}
		block := append([]byte(nil), sectionBytes[36:]...)
		_, wanted := remaining[cid]
		if !wanted || len(blocks) >= maximumBlocks {
			return nil, Invalid("unrequested or excessive CAR block")
		}
		delete(remaining, cid)
		if err := cid.Verify(block); err != nil {
			return nil, err
		}
		blocks[cid] = block
	}
	if len(remaining) != 0 {
		return nil, Invalid("missing CAR blocks")
	}
	return blocks, nil
}
