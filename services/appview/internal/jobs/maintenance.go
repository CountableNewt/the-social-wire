package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type MaintenanceResult struct {
	Jobs []Result `json:"jobs"`
}

var maintenanceCadences = []struct {
	name     string
	interval time.Duration
}{
	{"scheduling", 0},
	{"discovery", 0},
	{"drafts-cleanup", 0},
	{"author-history", 0},
	{"read-state-recovery", 0},
	{"rss-poll", 30 * time.Minute},
	{"ttl-cleanup", time.Hour},
	{"analytics-cleanup", 24 * time.Hour},
}

// RunMaintenance shares one schedule invocation across all enabled actions.
// Frequent actions run on every dispatch: a jittered dispatch can arrive less
// than fifteen minutes after its predecessor and must still publish due posts.
// Existing action leases and mutation fences also protect manual invocations.
func (r *Runner) RunMaintenance(ctx context.Context) (MaintenanceResult, error) {
	return r.runMaintenanceAt(ctx, time.Now())
}

func (r *Runner) runMaintenanceAt(ctx context.Context, now time.Time) (MaintenanceResult, error) {
	result := MaintenanceResult{Jobs: []Result{}}
	if r == nil {
		return result, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	var names []string
	var intervals []time.Duration
	for _, action := range maintenanceCadences {
		if _, ok := r.actions[action.name]; ok {
			names = append(names, action.name)
			intervals = append(intervals, action.interval)
		}
	}
	result.Jobs = make([]Result, len(names))
	failures := make([]error, len(names))
	var group sync.WaitGroup
	for i, name := range names {
		group.Add(1)
		go func() {
			defer group.Done()
			result.Jobs[i], failures[i] = r.runCadenced(ctx, name, intervals[i], now)
		}()
	}
	group.Wait()
	return result, errors.Join(failures...)
}

func (r *Runner) runCadenced(ctx context.Context, name string, interval time.Duration, now time.Time) (Result, error) {
	if interval == 0 {
		return r.Run(ctx, name)
	}
	var token [24]byte
	if _, err := rand.Read(token[:]); err != nil {
		return Result{Job: name}, err
	}
	owner, key := hex.EncodeToString(token[:]), "schedule:"+name
	claimed, err := r.claim(ctx, key, owner, now, now.Add(max(interval, r.actions[name].Timeout+30*time.Second)))
	if err != nil || !claimed {
		return Result{Job: name}, err
	}
	result, err := r.Run(ctx, name)
	if err != nil || !result.Completed {
		// A failure or competing manual run must not suppress the next dispatch.
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, r.release(release, key, owner))
	}
	return result, err
}
