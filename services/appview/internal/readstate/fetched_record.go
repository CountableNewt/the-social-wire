package readstate

// FetchedRecord is a getRecord response: the returned URI and CID plus the
// re-serialized record value.
type FetchedRecord struct {
	URI  string
	CID  string
	JSON []byte
}

// Verified binds the record to at://viewer/collection/key and verifies its
// CID, returning the JSON that is now safe to decode.
func (r FetchedRecord) Verified(viewerDID, collection, key string) ([]byte, error) {
	if r.URI != "at://"+viewerDID+"/"+collection+"/"+key {
		return nil, ErrInvalidReference
	}
	if err := VerifyRecordCID(r.JSON, r.CID); err != nil {
		return nil, err
	}
	return r.JSON, nil
}

// VerifiedManifest verifies and decodes the viewer's manifest record.
func (r FetchedRecord) VerifiedManifest(viewerDID string) (Manifest, error) {
	data, err := r.Verified(viewerDID, ManifestCollection, "self")
	if err != nil {
		return Manifest{}, err
	}
	return DecodeManifest(data)
}

// VerifiedChunk verifies and decodes a v1 chunk record.
func (r FetchedRecord) VerifiedChunk(viewerDID, key string) (Chunk, error) {
	data, err := r.Verified(viewerDID, ChunkCollection, key)
	if err != nil {
		return Chunk{}, err
	}
	return DecodeChunk(data)
}
