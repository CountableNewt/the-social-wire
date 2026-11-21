package readstate

// ChunkCollection is the immutable chunk record collection.
const ChunkCollection = "app.thesocialwire.readStateChunk"

// Chunk is a v1 chunk, immutable after publication. New operations prepend
// chunks and reuse the old chain.
type Chunk struct {
	Type       string
	Version    int
	Operations []Operation
	Previous   *Reference
	// AllowsRepacking is false when the decoded record carried unknown fields
	// a typed re-encode would discard. It is never encoded.
	AllowsRepacking bool
}

// NewChunk builds a v1 chunk.
func NewChunk(operations []Operation, previous *Reference) Chunk {
	if operations == nil {
		operations = []Operation{}
	}
	return Chunk{Type: ChunkCollection, Version: 1, Operations: operations, Previous: previous, AllowsRepacking: true}
}

type chunkWire struct {
	Type       string      `json:"$type"`
	Version    int         `json:"version"`
	Operations []Operation `json:"operations"`
	Previous   *Reference  `json:"previous,omitempty"`
}

// MarshalJSON uses Swift's key order.
func (c Chunk) MarshalJSON() ([]byte, error) {
	operations := c.Operations
	if operations == nil {
		operations = []Operation{}
	}
	return marshalNoEscape(chunkWire{Type: c.Type, Version: c.Version, Operations: operations, Previous: c.Previous})
}

// UnmarshalJSON decodes with exact keys and records whether the shape is fully known.
func (c *Chunk) UnmarshalJSON(data []byte) error {
	value, err := DecodeChunk(data)
	if err != nil {
		return err
	}
	*c = value
	return nil
}

// DecodeChunk decodes a v1 chunk record.
func DecodeChunk(data []byte) (Chunk, error) {
	value, err := parseObject(data)
	if err != nil {
		return Chunk{}, err
	}
	var result Chunk
	if result.Type, err = value.str("$type"); err != nil {
		return Chunk{}, err
	}
	if result.Version, err = value.int("version"); err != nil {
		return Chunk{}, err
	}
	items, err := value.array("operations")
	if err != nil {
		return Chunk{}, err
	}
	result.Operations = make([]Operation, 0, len(items))
	for _, item := range items {
		operationValue, err := parseObject(item)
		if err != nil {
			return Chunk{}, err
		}
		operation, err := decodeOperation(operationValue)
		if err != nil {
			return Chunk{}, err
		}
		result.Operations = append(result.Operations, operation)
	}
	if result.Previous, err = decodeOptionalReference(value, "previous"); err != nil {
		return Chunk{}, err
	}
	if result.AllowsRepacking, err = knownChunkShape(value); err != nil {
		return Chunk{}, err
	}
	return result, nil
}

// knownChunkShape is ReadStateRecordShape.knownChunk: unknown extension fields
// remain safe in immutable old chunks, but such generations must not be
// densely repacked.
func knownChunkShape(value object) (bool, error) {
	if !value.onlyKeys("$type", "version", "operations", "previous") {
		return false, nil
	}
	if value.present("previous") {
		previous, err := parseObject(value["previous"])
		if err != nil {
			return false, err
		}
		if !previous.onlyKeys("uri", "cid") {
			return false, nil
		}
	}
	items, err := value.array("operations")
	if err != nil {
		return false, err
	}
	for _, item := range items {
		operation, err := parseObject(item)
		if err != nil {
			return false, err
		}
		if !operation.onlyKeys("actionId", "sequence", "state", "actedAt", "selection",
			"boundaries", "subjectUris", "calendar") {
			return false, nil
		}
		if operation.present("calendar") {
			calendar, err := parseObject(operation["calendar"])
			if err != nil {
				return false, err
			}
			if !calendar.onlyKeys("cutoff", "timeZone", "referenceDate") {
				return false, nil
			}
		}
		if operation.present("boundaries") {
			boundaries, err := operation.array("boundaries")
			if err != nil {
				return false, err
			}
			for _, raw := range boundaries {
				boundary, err := parseObject(raw)
				if err != nil {
					return false, err
				}
				if !boundary.onlyKeys("scope", "createdAt", "entryId") {
					return false, nil
				}
				scope, err := parseObject(boundary["scope"])
				if err != nil {
					return false, err
				}
				if !scope.onlyKeys("publicationId", "authorDid", "publicationSiteKeys") {
					return false, nil
				}
			}
		}
	}
	return true, nil
}
