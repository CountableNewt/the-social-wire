package readstate

import (
	"context"
	"fmt"
)

// Generation load limits.
const (
	DefaultMaximumChunks = 4096
	DefaultMaximumBytes  = 16 * 1024 * 1024
)

// LoadLimits bounds a rebuild; zero fields use the defaults.
type LoadLimits struct {
	MaximumChunks int
	MaximumBytes  int
}

func (l LoadLimits) normalized() LoadLimits {
	if l.MaximumChunks == 0 {
		l.MaximumChunks = DefaultMaximumChunks
	}
	if l.MaximumBytes == 0 {
		l.MaximumBytes = DefaultMaximumBytes
	}
	return l
}

// LoadGeneration loads a v1 chain from typed chunks. fetch must fetch this
// exact URI/CID from the viewer's resolved PDS and verify its content CID;
// never implement it by trusting a client-provided body. Resource exhaustion
// fails the whole rebuild; it never publishes a partial prefix.
func LoadGeneration(ctx context.Context, manifest Manifest, viewerDID string, limits LoadLimits,
	fetch func(context.Context, Reference) (Chunk, error)) (*Projection, error) {
	limits = limits.normalized()
	if err := ValidateManifest(manifest, viewerDID); err != nil {
		return nil, err
	}
	if manifest.Version != 1 {
		return nil, ErrInvalidRecord
	}
	reference := manifest.Head
	visited := map[Reference]struct{}{}
	var operations []Operation
	bytes := 0
	allowsRepacking := true
	for reference != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current := *reference
		if _, seen := visited[current]; seen {
			return nil, ErrInvalidReference
		}
		visited[current] = struct{}{}
		if len(visited) > limits.MaximumChunks {
			return nil, ErrSizeLimit
		}
		chunk, err := fetch(ctx, current)
		if err != nil {
			return nil, err
		}
		if err := ValidateChunk(chunk, viewerDID); err != nil {
			return nil, err
		}
		encoded, err := EncodedByteCount(chunk)
		if err != nil {
			return nil, err
		}
		if chunk.AllowsRepacking {
			bytes += encoded
		} else {
			bytes += MaximumRecordBytes
		}
		if bytes > limits.MaximumBytes {
			return nil, ErrSizeLimit
		}
		operations = append(operations, chunk.Operations...)
		allowsRepacking = allowsRepacking && chunk.AllowsRepacking
		reference = chunk.Previous
	}
	if operations == nil {
		operations = []Operation{}
	}
	return NewProjection(operations, manifest.LastSequence, ProjectionSource{
		ChunkCount: len(visited), Bytes: bytes, DisallowRepacking: !allowsRepacking, References: visited,
	})
}

// LoadRecords loads either protocol from raw records. fetch binds the raw
// record to the viewer URI and verifies its CID. Both versions retain
// full-chain completeness and bounded raw-byte accounting.
func LoadRecords(ctx context.Context, manifest Manifest, viewerDID string, limits LoadLimits,
	fetch func(context.Context, Reference) ([]byte, error)) (*Projection, error) {
	limits = limits.normalized()
	if err := ValidateManifest(manifest, viewerDID); err != nil {
		return nil, err
	}
	if manifest.Version == 1 {
		return LoadGeneration(ctx, manifest, viewerDID, limits, func(ctx context.Context, reference Reference) (Chunk, error) {
			data, err := fetch(ctx, reference)
			if err != nil {
				return Chunk{}, err
			}
			if len(data) > MaximumRecordBytes {
				return Chunk{}, ErrSizeLimit
			}
			return DecodeChunk(data)
		})
	}
	visited := map[Reference]struct{}{}
	var fragments []V2Fragment
	var devices []DeviceReceipt
	var legacy []LegacyActionReceipt
	bytes := 0
	bytesByKind := map[string]int{}
	chunksByKind := map[string]int{}
	roots := []struct {
		kind V2ChunkKind
		root *Reference
	}{
		{V2KindState, manifest.StateHead},
		{V2KindDevices, manifest.DevicesHead},
		{V2KindLegacyReceipts, manifest.LegacyReceiptsHead},
	}
	for _, root := range roots {
		reference := root.root
		for reference != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			current := *reference
			if err := ValidateReference(current, viewerDID); err != nil {
				return nil, err
			}
			if _, seen := visited[current]; seen {
				return nil, ErrInvalidReference
			}
			visited[current] = struct{}{}
			if len(visited) > limits.MaximumChunks {
				return nil, ErrSizeLimit
			}
			data, err := fetch(ctx, current)
			if err != nil {
				return nil, err
			}
			bytes += len(data)
			bytesByKind[string(root.kind)] += len(data)
			chunksByKind[string(root.kind)]++
			if len(data) > MaximumRecordBytes || bytes > limits.MaximumBytes {
				return nil, ErrSizeLimit
			}
			chunk, err := DecodeV2Chunk(data)
			if err != nil {
				return nil, err
			}
			if err := chunk.Validate(viewerDID); err != nil {
				return nil, err
			}
			if chunk.Kind != root.kind {
				return nil, ErrInvalidRecord
			}
			fragments = append(fragments, chunk.Fragments...)
			devices = append(devices, chunk.DeviceReceipts...)
			legacy = append(legacy, chunk.LegacyReceipts...)
			reference = chunk.Previous
		}
	}
	devicesByID := map[string]DeviceReceipt{}
	for _, receipt := range devices {
		devicesByID[receipt.DeviceID] = receipt
	}
	legacyByID := map[string]LegacyActionReceipt{}
	legacySequences := map[int64]struct{}{}
	var legacySequence int64
	for _, receipt := range legacy {
		legacyByID[receipt.ActionID] = receipt
		legacySequences[receipt.OriginalSequence] = struct{}{}
		legacySequence = max(legacySequence, receipt.OriginalSequence)
	}
	if len(devicesByID) != len(devices) || len(legacyByID) != len(legacy) || len(legacySequences) != len(legacy) {
		return nil, ErrConflictingSequence
	}
	for _, receipt := range devices {
		if receipt.CommittedCounter != 0 {
			continue
		}
		empty, err := NewDeviceReceipt(viewerDID, receipt.DeviceID)
		if err != nil {
			return nil, err
		}
		if empty != receipt {
			return nil, ErrConflictingSequence
		}
	}
	identityByAction := map[string]V2Fragment{}
	actionByCounter := map[string]string{}
	for _, fragment := range fragments {
		operation := fragment.Operation
		if previous, ok := identityByAction[operation.ActionID]; ok {
			if previous.IntentHash != fragment.IntentHash || !stringPtrEqual(previous.DeviceID, fragment.DeviceID) ||
				!int64PtrEqual(previous.DeviceCounter, fragment.DeviceCounter) ||
				previous.Operation.Selection != operation.Selection {
				return nil, ErrConflictingSequence
			}
		}
		identityByAction[operation.ActionID] = fragment
		if fragment.DeviceID != nil && fragment.DeviceCounter != nil {
			receipt, ok := devicesByID[*fragment.DeviceID]
			_, isLegacy := legacyByID[operation.ActionID]
			if !ok || *fragment.DeviceCounter > receipt.CommittedCounter || operation.Sequence <= legacySequence || isLegacy {
				return nil, ErrIncompleteGeneration
			}
			key := fmt.Sprintf("%s:%d", *fragment.DeviceID, *fragment.DeviceCounter)
			if existing, ok := actionByCounter[key]; ok && existing != operation.ActionID {
				return nil, ErrConflictingSequence
			}
			actionByCounter[key] = operation.ActionID
		} else {
			receipt, ok := legacyByID[operation.ActionID]
			if !ok || receipt.OriginalSequence != operation.Sequence || receipt.OriginalIntentHash != fragment.IntentHash {
				return nil, ErrIncompleteGeneration
			}
		}
	}
	for _, receipt := range legacy {
		if receipt.OriginalSequence > manifest.LastSequence {
			return nil, ErrInvalidRecord
		}
	}
	operations := make([]Operation, 0, len(fragments))
	for _, fragment := range fragments {
		operations = append(operations, fragment.Operation)
	}
	return NewProjection(operations, manifest.LastSequence, ProjectionSource{
		ChunkCount: len(visited), Bytes: bytes, References: visited, ProtocolVersion: 2,
		V2Fragments: fragments, DeviceReceipts: devices, LegacyReceipts: legacy,
		BytesByKind: bytesByKind, ChunksByKind: chunksByKind,
	})
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func int64PtrEqual(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
