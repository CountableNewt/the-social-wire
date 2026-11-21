package readstate

import (
	"slices"
	"time"
)

// ProjectionSource describes where a projection's operations came from.
// Zero values mean v1, repackable, with no recorded source.
type ProjectionSource struct {
	ChunkCount        int
	Bytes             int
	DisallowRepacking bool
	References        map[Reference]struct{}
	ProtocolVersion   int
	V2Fragments       []V2Fragment
	DeviceReceipts    []DeviceReceipt
	LegacyReceipts    []LegacyActionReceipt
	BytesByKind       map[string]int
	ChunksByKind      map[string]int
}

type scopedRule struct {
	boundary  Boundary
	createdAt time.Time
	operation Operation
}

// Projection is immutable and installed only after the complete referenced
// chain has verified (ReadStateProjection).
type Projection struct {
	exact  map[string]Operation
	scoped map[string][]scopedRule

	LastSequence       int64
	ActionIDs          map[string]struct{}
	Operations         []Operation
	SourceChunkCount   int
	SourceBytesByKind  map[string]int
	SourceChunksByKind map[string]int
	SourceBytes        int
	AllowsRepacking    bool
	SourceReferences   map[Reference]struct{}
	ProtocolVersion    int
	V2Fragments        []V2Fragment
	DeviceReceipts     []DeviceReceipt
	LegacyReceipts     []LegacyActionReceipt
}

// NewProjection validates operations and indexes them for resolution.
func NewProjection(operations []Operation, lastSequence int64, source ProjectionSource) (*Projection, error) {
	if lastSequence < 0 || lastSequence > MaximumSequence {
		return nil, ErrInvalidRecord
	}
	protocol := source.ProtocolVersion
	if protocol == 0 {
		protocol = 1
	}
	exact := map[string]Operation{}
	scoped := map[string][]scopedRule{}
	sequences := map[int64]Operation{}
	actions := map[string]int64{}
	var maxSequence int64
	for _, operation := range operations {
		if err := ValidateOperation(operation); err != nil {
			return nil, err
		}
		if operation.Sequence > lastSequence {
			return nil, ErrInvalidRecord
		}
		if other, ok := sequences[operation.Sequence]; ok &&
			(other.ActionID != operation.ActionID || other.State != operation.State ||
				other.ActedAt != operation.ActedAt || !calendarEqual(other.Calendar, operation.Calendar)) {
			return nil, ErrConflictingSequence
		}
		if sequence, ok := actions[operation.ActionID]; ok && sequence != operation.Sequence {
			return nil, ErrConflictingSequence
		}
		sequences[operation.Sequence] = operation
		actions[operation.ActionID] = operation.Sequence
		maxSequence = max(maxSequence, operation.Sequence)
		for _, uri := range operation.SubjectURIs {
			if current, ok := exact[uri]; !ok || current.Sequence < operation.Sequence {
				exact[uri] = operation
			}
		}
		for _, boundary := range operation.Boundaries {
			createdAt, err := ParseDate(boundary.CreatedAt)
			if err != nil {
				return nil, err
			}
			scoped[boundary.Scope.AuthorDID] = append(scoped[boundary.Scope.AuthorDID],
				scopedRule{boundary: boundary, createdAt: createdAt, operation: operation})
		}
	}
	if (protocol != 1 && protocol != 2) || (protocol != 2 && maxSequence != lastSequence) {
		return nil, ErrIncompleteGeneration
	}
	actionIDs := make(map[string]struct{}, len(actions))
	for actionID := range actions {
		actionIDs[actionID] = struct{}{}
	}
	return &Projection{
		exact: exact, scoped: scoped, LastSequence: lastSequence, ActionIDs: actionIDs,
		Operations: operations, SourceChunkCount: source.ChunkCount, SourceBytes: source.Bytes,
		SourceBytesByKind: source.BytesByKind, SourceChunksByKind: source.ChunksByKind,
		AllowsRepacking: !source.DisallowRepacking, SourceReferences: source.References,
		ProtocolVersion: protocol, V2Fragments: source.V2Fragments,
		DeviceReceipts: source.DeviceReceipts, LegacyReceipts: source.LegacyReceipts,
	}, nil
}

func calendarEqual(a, b *CalendarSelection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Subject is an indexed entry evaluated against a projection.
type Subject struct {
	URI             string
	AuthorDID       string
	PublicationSite *string
	CreatedAt       time.Time
}

// Resolution is the effective state of one subject.
type Resolution struct {
	IsRead   bool
	ReadAt   *string
	Sequence int64
	ActionID *string
}

// Resolve applies the highest committed sequence among the subject's exact
// operation and every matching boundary.
func (p *Projection) Resolve(subject Subject) Resolution {
	var latest *Operation
	if operation, ok := p.exact[subject.URI]; ok {
		latest = &operation
	}
	for _, rule := range p.scoped[subject.AuthorDID] {
		var current int64
		if latest != nil {
			current = latest.Sequence
		}
		if rule.operation.Sequence <= current {
			continue
		}
		keys := rule.boundary.Scope.PublicationSiteKeys
		if len(keys) > 0 && (subject.PublicationSite == nil || !slices.Contains(keys, *subject.PublicationSite)) {
			continue
		}
		covered := subject.CreatedAt.Before(rule.createdAt) || (subject.CreatedAt.Equal(rule.createdAt) &&
			(rule.boundary.EntryID == nil || !(*rule.boundary.EntryID < subject.URI)))
		if !covered {
			continue
		}
		operation := rule.operation
		latest = &operation
	}
	if latest == nil {
		return Resolution{}
	}
	resolution := Resolution{IsRead: latest.State == StateRead, Sequence: latest.Sequence}
	actionID := latest.ActionID
	resolution.ActionID = &actionID
	if resolution.IsRead {
		readAt := latest.ActedAt
		resolution.ReadAt = &readAt
	}
	return resolution
}
