package repo

import (
	"context"
	"time"
)

// Snapshot reader ceilings; configured values above these (or <= 0) clamp to them.
const (
	snapshotMaximumBytesCeiling    = 32 * 1024 * 1024
	snapshotMaximumBlocksCeiling   = 100_000
	snapshotMaximumRecordsCeiling  = 20_000
	snapshotMaximumRequestsCeiling = 1_024
	snapshotMaximumDepthCeiling    = 64
	snapshotTimeoutCeiling         = 120 * time.Second
)

// SnapshotReader reads a repository through com.atproto.sync.* and verifies
// the DID document binding, signed commit, MST, and every record block.
type SnapshotReader struct {
	Transport       HTTPTransport
	PLCBase         string
	MaximumBytes    int
	MaximumBlocks   int
	MaximumRecords  int
	MaximumRequests int
	MaximumDepth    int
	Timeout         time.Duration
	Now             func() time.Time
}

// NewSnapshotReader returns a reader with the Swift defaults over transport.
// An empty plcBase defaults to https://plc.directory.
func NewSnapshotReader(transport HTTPTransport, plcBase string) SnapshotReader {
	if plcBase == "" {
		plcBase = "https://plc.directory"
	}
	return SnapshotReader{
		Transport:       transport,
		PLCBase:         plcBase,
		MaximumBytes:    snapshotMaximumBytesCeiling,
		MaximumBlocks:   snapshotMaximumBlocksCeiling,
		MaximumRecords:  snapshotMaximumRecordsCeiling,
		MaximumRequests: snapshotMaximumRequestsCeiling,
		MaximumDepth:    snapshotMaximumDepthCeiling,
		Timeout:         snapshotTimeoutCeiling,
		Now:             time.Now,
	}
}

// NewPublicSnapshotReader returns a reader over PublicHTTPTransport.
func NewPublicSnapshotReader(plcBase, userAgent string) SnapshotReader {
	return NewSnapshotReader(PublicHTTPTransport{UserAgent: userAgent}, plcBase)
}

// Read fetches and verifies the records of the given collections for did. The
// whole attempt, including retries, is bounded by Timeout (max 120s).
func (r SnapshotReader) Read(ctx context.Context, did string, collections []string) (Snapshot, error) {
	if len(collections) == 0 {
		return Snapshot{}, Invalid("invalid collection")
	}
	set := make(map[string]struct{}, len(collections))
	for _, collection := range collections {
		if !ValidRecordPath(collection + "/record") {
			return Snapshot{}, Invalid("invalid collection")
		}
		set[collection] = struct{}{}
	}
	configuration := r
	configuration.MaximumBytes = boundLimit(r.MaximumBytes, snapshotMaximumBytesCeiling)
	configuration.MaximumBlocks = boundLimit(r.MaximumBlocks, snapshotMaximumBlocksCeiling)
	configuration.MaximumRecords = boundLimit(r.MaximumRecords, snapshotMaximumRecordsCeiling)
	configuration.MaximumRequests = boundLimit(r.MaximumRequests, snapshotMaximumRequestsCeiling)
	configuration.MaximumDepth = boundLimit(r.MaximumDepth, snapshotMaximumDepthCeiling)
	if configuration.Now == nil {
		configuration.Now = time.Now
	}
	duration := r.Timeout
	if duration <= 0 || duration > snapshotTimeoutCeiling {
		duration = snapshotTimeoutCeiling
	}
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		snapshot Snapshot
		err      error
	}
	done := make(chan result, 1)
	go func() {
		attempt := newSnapshotAttempt(configuration)
		snapshot, err := attempt.read(attemptCtx, did, set)
		done <- result{snapshot, err}
	}()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case outcome := <-done:
		return outcome.snapshot, outcome.err
	case <-timer.C:
		cancel()
		<-done
		return Snapshot{}, Invalid("snapshot deadline exceeded")
	}
}

func boundLimit(value, ceiling int) int {
	if value > 0 {
		return min(value, ceiling)
	}
	return ceiling
}
