package reader

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRemoteReadBudgetAllowsColdSetupWithoutRelaxingSQLite(t *testing.T) {
	start := time.Now()
	local, stopLocal := readerQueryContext(context.Background(), false)
	defer stopLocal()
	remote, stopRemote := readerQueryContext(context.Background(), true)
	defer stopRemote()
	localDeadline, localBounded := local.Deadline()
	remoteDeadline, remoteBounded := remote.Deadline()
	// Evaluate a three-second cold setup against both policies without sleeping:
	// it must miss the local deadline and still fit inside the remote deadline.
	coldSetupFinishes := start.Add(3 * time.Second)
	if !localBounded || !remoteBounded || !localDeadline.Before(coldSetupFinishes) || !remoteDeadline.After(coldSetupFinishes) {
		t.Fatal("cold remote setup was rejected or SQLite's tighter deadline was lost")
	}
	if remoteDeadline.After(start.Add(6 * time.Second)) {
		t.Fatal("remote reads are no longer tightly bounded")
	}
}

func TestReaderBudgetsPreserveEarlierParentDeadlineAndCancellation(t *testing.T) {
	for _, remote := range []bool{false, true} {
		deadline := time.Now().Add(time.Second)
		parent, stopParent := context.WithDeadline(context.Background(), deadline)
		child, stopChild := readerQueryContext(parent, remote)
		got, bounded := child.Deadline()
		if !bounded || !got.Equal(deadline) {
			t.Fatal("reader extended an earlier caller deadline")
		}
		stopParent()
		select {
		case <-child.Done():
			if !errors.Is(child.Err(), context.Canceled) {
				t.Fatal("caller cancellation cause changed")
			}
		default:
			t.Fatal("reader ignored caller cancellation")
		}
		stopChild()
	}
}
