package operations

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"
)

// BatchExporter persists one batch of signals.
type BatchExporter func(ctx context.Context, signals []Signal) error

// TelemetrySnapshot is OperationsTelemetryBufferSnapshot.
type TelemetrySnapshot struct {
	QueueDepth             int
	InFlightCount          int
	Capacity               int
	DroppedCount           int
	ConsecutiveFailures    int
	LastSuccessfulExportAt *time.Time
	LastDropAt             *time.Time
	LastDropRecoveredAt    *time.Time
}

// TelemetryBufferOptions tune a buffer; zero values take the Swift defaults
// (capacity 4096, batch 100, 5 attempts, 1 second batching delay).
type TelemetryBufferOptions struct {
	Capacity         int
	BatchSize        int
	MaxRetryAttempts int
	// BatchDelay is how long a partial batch may accumulate; negative means zero.
	BatchDelay time.Duration
	// Now supplies wall-clock timestamps for snapshot evidence.
	Now    func() time.Time
	Logger *slog.Logger
}

// TelemetryBuffer is OperationsTelemetryBuffer: a bounded FIFO that drops
// (and counts) signals when full and exports batches with bounded retries.
// A nil *TelemetryBuffer accepts calls and does nothing, like Swift's optional
// `telemetry?.enqueue`.
type TelemetryBuffer struct {
	capacity         int
	batchSize        int
	maxRetryAttempts int
	batchDelay       time.Duration
	exporter         BatchExporter
	now              func() time.Time
	logger           *slog.Logger

	mu                  sync.Mutex
	queue               []queuedSignal
	inFlight            int
	retryNotBefore      time.Time
	droppedCount        int
	consecutiveFailures int
	lastSuccess         *time.Time
	lastDrop            *time.Time
	lastDropRecovered   *time.Time
}

type queuedSignal struct {
	signal     Signal
	enqueuedAt time.Time
}

// NewTelemetryBuffer exports into store with default options.
func NewTelemetryBuffer(store *Store, logger *slog.Logger) *TelemetryBuffer {
	return NewTelemetryBufferWithExporter(TelemetryBufferOptions{Logger: logger}, store.RecordTelemetryBatch)
}

// NewTelemetryBufferWithExporter builds a buffer over an arbitrary exporter.
func NewTelemetryBufferWithExporter(options TelemetryBufferOptions, exporter BatchExporter) *TelemetryBuffer {
	capacity := options.Capacity
	if capacity == 0 {
		capacity = 4_096
	}
	capacity = max(1, capacity)
	batchSize := options.BatchSize
	if batchSize == 0 {
		batchSize = 100
	}
	attempts := options.MaxRetryAttempts
	if attempts == 0 {
		attempts = 5
	}
	delay := options.BatchDelay
	if delay == 0 {
		delay = time.Second
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TelemetryBuffer{
		capacity:         capacity,
		batchSize:        max(1, min(batchSize, capacity)),
		maxRetryAttempts: max(1, attempts),
		batchDelay:       max(0, delay),
		exporter:         exporter,
		now:              now,
		logger:           logger,
	}
}

// Enqueue queues signal, or drops and counts it when the buffer (queued plus
// in flight) is at capacity.
func (b *TelemetryBuffer) Enqueue(signal Signal) bool {
	if b == nil {
		return false
	}
	return b.enqueueAt(signal, time.Now())
}

func (b *TelemetryBuffer) enqueueAt(signal Signal, at time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.queue)+b.inFlight >= b.capacity {
		b.recordDropLocked(1)
		return false
	}
	b.queue = append(b.queue, queuedSignal{signal: signal, enqueuedAt: at})
	return true
}

// Run exports ready batches until ctx is cancelled, polling every 100ms while idle.
func (b *TelemetryBuffer) Run(ctx context.Context) {
	if b == nil {
		return
	}
	for ctx.Err() == nil {
		if b.flushIfReady(ctx, time.Now()) == 0 {
			// Polling also wakes a newly full batch promptly while a partial batch accumulates.
			sleep(ctx, 100*time.Millisecond)
		}
	}
}

func (b *TelemetryBuffer) flushIfReady(ctx context.Context, at time.Time) int {
	b.mu.Lock()
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return 0
	}
	ready := len(b.queue) >= b.batchSize || at.Sub(b.queue[0].enqueuedAt) >= b.batchDelay
	b.mu.Unlock()
	if !ready {
		return 0
	}
	return b.flushAt(ctx, at)
}

// FlushOnce exports one batch immediately, ignoring the batching delay, and
// returns how many signals were exported.
func (b *TelemetryBuffer) FlushOnce(ctx context.Context) int {
	if b == nil {
		return 0
	}
	return b.flushAt(ctx, time.Now())
}

func (b *TelemetryBuffer) flushAt(ctx context.Context, at time.Time) int {
	// Keep the original batch counted against capacity and preserve FIFO
	// across every retry; concurrent flushes return 0 while one is in flight.
	b.mu.Lock()
	if b.inFlight != 0 || len(b.queue) == 0 {
		b.mu.Unlock()
		return 0
	}
	if !b.retryNotBefore.IsZero() && at.Before(b.retryNotBefore) {
		b.mu.Unlock()
		return 0
	}
	count := min(b.batchSize, len(b.queue))
	batch := make([]Signal, count)
	for i := range count {
		batch[i] = b.queue[i].signal
	}
	b.queue = append([]queuedSignal(nil), b.queue[count:]...)
	b.inFlight = count
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.inFlight = 0
		b.mu.Unlock()
	}()

	for attempt := 0; attempt < b.maxRetryAttempts; attempt++ {
		err := b.exporter(ctx, batch)
		b.mu.Lock()
		if err == nil {
			b.retryNotBefore = time.Time{}
			b.consecutiveFailures = 0
			exportedAt := b.now()
			b.lastSuccess = &exportedAt
			// Only a successful drain establishes recovery; keep it latched.
			if len(b.queue) == 0 && b.lastDropRecovered == nil && b.lastDrop != nil && exportedAt.After(*b.lastDrop) {
				b.lastDropRecovered = &exportedAt
			}
			b.mu.Unlock()
			return count
		}
		b.consecutiveFailures++
		if !canRetryTelemetry(err) {
			// An unknown outcome must remain visible instead of replaying an
			// additive batch and potentially counting every metric twice.
			b.recordDropLocked(count)
			b.mu.Unlock()
			b.logger.Error("Telemetry export stopped without a safe retry",
				"error_type", ErrorCategory(err), "batch_size", strconv.Itoa(count))
			return 0
		}
		if attempt+1 >= b.maxRetryAttempts {
			// SQLite exports never prove a rolled-back contention outcome, so
			// the Postgres-only deferral path never applies here.
			b.recordDropLocked(count)
			b.mu.Unlock()
			b.logger.Error("Telemetry export exhausted bounded retries",
				"error_type", ErrorCategory(err), "batch_size", strconv.Itoa(count))
			return 0
		}
		b.mu.Unlock()
		base := min(5_000, 100*(1<<min(attempt, 5)))
		jitter := rand.IntN(max(1, base/4) + 1)
		sleep(ctx, time.Duration(base+jitter)*time.Millisecond)
	}
	return 0
}

// canRetryTelemetry is PostgresTelemetryRetryPolicy.canRetry for non-Postgres
// exporters: everything but cancellation keeps the bounded retry contract.
func canRetryTelemetry(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (b *TelemetryBuffer) recordDropLocked(count int) {
	b.droppedCount += count
	dropAt := b.now()
	b.lastDrop = &dropAt
	b.lastDropRecovered = nil
}

// PendingCount is the number of queued (not in-flight) signals.
func (b *TelemetryBuffer) PendingCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// DroppedCount is the lifetime number of dropped signals.
func (b *TelemetryBuffer) DroppedCount() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.droppedCount
}

// Snapshot reports the buffer's export evidence.
func (b *TelemetryBuffer) Snapshot() TelemetrySnapshot {
	if b == nil {
		return TelemetrySnapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return TelemetrySnapshot{
		QueueDepth:             len(b.queue),
		InFlightCount:          b.inFlight,
		Capacity:               b.capacity,
		DroppedCount:           b.droppedCount,
		ConsecutiveFailures:    b.consecutiveFailures,
		LastSuccessfulExportAt: b.lastSuccess,
		LastDropAt:             b.lastDrop,
		LastDropRecoveredAt:    b.lastDropRecovered,
	}
}

// sleep waits d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
