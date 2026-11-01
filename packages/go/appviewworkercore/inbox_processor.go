package appviewworkercore

import (
	"context"
	"errors"
	"time"

	"github.com/stygian-tech/the-social-wire/packages/go/thinappviewcore"
)

// InboxRepository is the fenced persistence boundary. Processors must never
// acknowledge an event before projection mutations have completed.
type InboxRepository interface {
	Applied(context.Context, thinappviewcore.InboxItem, string, time.Time, time.Time) error
	Retry(context.Context, thinappviewcore.InboxItem, string, string, string, time.Time, time.Time) error
	Renew(context.Context, thinappviewcore.InboxItem, string, time.Time, time.Time) error
	DeadLetter(context.Context, thinappviewcore.InboxItem, string, string, string, time.Time, time.Time) error
}
type EventProjector interface {
	Apply(context.Context, thinappviewcore.InboxItem) error
}

type InboxProcessor struct {
	Repository                                                              InboxRepository
	Projector                                                               EventProjector
	WorkerID                                                                string
	LeaseDuration, ProjectionTimeout, AppliedRetention, DeadLetterRetention time.Duration
	Now                                                                     func() time.Time
	Jitter                                                                  func() float64
}

func InboxRetryDelay(attempt int, jitter float64) time.Duration {
	exponent := max(0, min(attempt-1, 7))
	base := min(30., .25*float64(uint64(1)<<exponent))
	return time.Duration(min(30., base+base*.25*max(0, min(1, jitter))) * float64(time.Second))
}

// Process cancels projection immediately if its claim cannot be renewed. Retry
// persistence uses the original token, so lease loss cannot mutate a new claim.
func (p InboxProcessor) Process(ctx context.Context, item thinappviewcore.InboxItem) error {
	if p.Repository == nil || p.Projector == nil || p.WorkerID == "" || p.LeaseDuration <= 0 || p.ProjectionTimeout <= 0 {
		return errors.New("invalid inbox processor configuration")
	}
	now := p.Now
	if now == nil {
		now = time.Now
	}
	jitter := p.Jitter
	if jitter == nil {
		jitter = func() float64 { return .5 }
	}
	applyCtx, cancel := context.WithTimeout(ctx, p.ProjectionTimeout)
	defer cancel()
	projectionDone := make(chan error, 1)
	renewDone := make(chan error, 1)
	go func() { projectionDone <- p.Projector.Apply(applyCtx, item) }()
	go func() {
		ticker := time.NewTicker(max(time.Millisecond, p.LeaseDuration/3))
		defer ticker.Stop()
		for {
			select {
			case <-applyCtx.Done():
				renewDone <- nil
				return
			case <-ticker.C:
				at := now()
				if err := p.Repository.Renew(applyCtx, item, p.WorkerID, at.Add(p.LeaseDuration), at); err != nil {
					renewDone <- err
					return
				}
			}
		}
	}()
	var applyErr error
	select {
	case applyErr = <-projectionDone:
		cancel()
		renewErr := <-renewDone
		if applyErr == nil && renewErr != nil {
			applyErr = renewErr
		}
	case applyErr = <-renewDone:
		cancel()
		projectionErr := <-projectionDone
		if applyErr == nil {
			applyErr = projectionErr
		}
	case <-applyCtx.Done():
		applyErr = applyCtx.Err()
		cancel()
		<-projectionDone
		<-renewDone
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(applyErr, thinappviewcore.ErrRecoveryYielded) {
		return nil
	}
	at := now()
	if applyErr == nil {
		return p.Repository.Applied(ctx, item, p.WorkerID, at.Add(p.AppliedRetention), at)
	}
	attempt := item.AttemptCount + 1
	category := "projection_failed"
	if errors.Is(applyErr, context.DeadlineExceeded) {
		category = "projection_timed_out"
	}
	if attempt >= 10 {
		return p.Repository.DeadLetter(ctx, item, p.WorkerID, category, category, at.Add(p.DeadLetterRetention), at)
	}
	return p.Repository.Retry(ctx, item, p.WorkerID, category, category, at.Add(InboxRetryDelay(attempt, jitter())), at)
}
