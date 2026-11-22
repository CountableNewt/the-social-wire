package sqlitedb

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"

	turso "turso.tech/database/tursogo-serverless"
)

type idleTestSession struct {
	fakeTursoSession
	afterCall func()
}

func (s *idleTestSession) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	r, e := s.fakeTursoSession.ExecContext(ctx, q, args)
	if s.afterCall != nil {
		s.afterCall()
	}
	return r, e
}
func (s *idleTestSession) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if s.afterCall != nil {
		s.afterCall()
	}
	return idleTestRows{}, nil
}
func (s *idleTestSession) Ping(context.Context) error {
	if s.afterCall != nil {
		s.afterCall()
	}
	return nil
}
func (s *idleTestSession) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	r, e := s.fakeTursoSession.PrepareContext(ctx, q)
	if s.afterCall != nil {
		s.afterCall()
	}
	return r, e
}
func (s *idleTestSession) DescribeContext(ctx context.Context, q string) (*turso.Description, error) {
	r, e := s.fakeTursoSession.DescribeContext(ctx, q)
	if s.afterCall != nil {
		s.afterCall()
	}
	return r, e
}

type idleTestRows struct{}

func (idleTestRows) Columns() []string         { return []string{"value"} }
func (idleTestRows) Close() error              { return nil }
func (idleTestRows) Next([]driver.Value) error { return io.EOF }

func idleConnectionFixture() (*tursoConnection, *idleTestSession, *time.Time) {
	clock := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	s := &idleTestSession{}
	c := &tursoConnection{remoteSession: s, maxIdle: 15 * time.Second, now: func() time.Time { return clock }}
	c.markActivity()
	return c, s, &clock
}

func TestIdleCheckoutRejectsBeforeSQLAndDoesNotRenewActivity(t *testing.T) {
	c, s, clock := idleConnectionFixture()
	last := c.lastActivity
	*clock = clock.Add(16 * time.Second)
	if err := c.ResetSession(t.Context()); !errors.Is(err, driver.ErrBadConn) {
		t.Fatal("stale session reused")
	}
	if len(s.queries) != 0 || len(s.described) != 0 || !c.lastActivity.Equal(last) {
		t.Fatal("stale checkout sent SQL or renewed activity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.ResetSession(ctx); !errors.Is(err, context.Canceled) || errors.Is(err, driver.ErrBadConn) {
		t.Fatal("canceled checkout lost its context error")
	}
	if !c.lastActivity.Equal(last) || len(s.queries) != 0 {
		t.Fatal("canceled checkout touched session")
	}
}

func TestActiveSessionSurvivesTotalAgeAndTracksOperationCompletion(t *testing.T) {
	c, s, clock := idleConnectionFixture()
	start := *clock
	s.afterCall = func() { *clock = clock.Add(20 * time.Second) }
	for range 4 {
		*clock = clock.Add(8 * time.Second)
		if err := c.ResetSession(t.Context()); err != nil {
			t.Fatal("active connection discarded based on total age")
		}
		if _, err := c.ExecContext(t.Context(), "INSERT INTO fixture VALUES(1)", nil); err != nil {
			t.Fatal(err)
		}
		if err := c.ResetSession(t.Context()); err != nil {
			t.Fatal("long operation was mistaken for idle time")
		}
	}
	if clock.Sub(start) <= 15*time.Second || len(s.queries) != 4 {
		t.Fatal("active session test did not span the old total lifetime")
	}
}

func TestBufferedRowsCloseDoesNotHideExpiredBackendSession(t *testing.T) {
	c, s, clock := idleConnectionFixture()
	s.afterCall = func() { *clock = clock.Add(20 * time.Second) }
	rows, err := c.QueryContext(t.Context(), "SELECT 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ResetSession(t.Context()); err != nil {
		t.Fatal("query execution time counted as idle")
	}
	*clock = clock.Add(16 * time.Second)
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.ResetSession(t.Context()); !errors.Is(err, driver.ErrBadConn) {
		t.Fatal("local buffered-row close renewed an expired remote session")
	}
}

func TestTransactionCompletionUpdatesBackendIdleClock(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(map[bool]string{true: "commit", false: "rollback"}[commit], func(t *testing.T) {
			c, s, clock := idleConnectionFixture()
			tx, err := c.BeginTx(t.Context(), driver.TxOptions{})
			if err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(10 * time.Second)
			s.afterCall = func() { *clock = clock.Add(20 * time.Second) }
			if commit {
				err = tx.Commit()
			} else {
				err = tx.Rollback()
			}
			if err != nil {
				t.Fatal(err)
			}
			*clock = clock.Add(14 * time.Second)
			if err := c.ResetSession(t.Context()); err != nil {
				t.Fatal("completion did not renew backend activity")
			}
			*clock = clock.Add(2 * time.Second)
			if err := c.ResetSession(t.Context()); !errors.Is(err, driver.ErrBadConn) {
				t.Fatal("transaction completion bypassed idle policy")
			}
		})
	}
}

func TestPrepareAndPingUpdateActivityButCanceledSQLDoesNot(t *testing.T) {
	c, s, clock := idleConnectionFixture()
	s.afterCall = func() { *clock = clock.Add(time.Second) }
	*clock = clock.Add(14 * time.Second)
	if _, err := c.PrepareContext(t.Context(), "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(14 * time.Second)
	if err := c.ResetSession(t.Context()); err != nil {
		t.Fatal("prepare did not update activity")
	}
	if err := c.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	last := c.lastActivity
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	*clock = clock.Add(time.Second)
	if _, err := c.ExecContext(ctx, "INSERT INTO fixture VALUES(1)", nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	if _, err := c.BeginTx(ctx, driver.TxOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("transaction cancellation ignored")
	}
	if err := c.Ping(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("ping cancellation ignored")
	}
	if !c.lastActivity.Equal(last) || len(s.queries) != 0 {
		t.Fatal("canceled SQL renewed activity or reached server")
	}
}

func TestUnknownMutationOutcomeIsNotMarkedReplayable(t *testing.T) {
	c, s, _ := idleConnectionFixture()
	s.failure = turso.ErrSessionLost
	if _, err := c.ExecContext(t.Context(), "INSERT INTO fixture VALUES(1)", nil); err == nil || errors.Is(err, driver.ErrBadConn) {
		t.Fatal("unknown mutation outcome enabled replay")
	}
	if len(s.queries) != 1 {
		t.Fatal("mutation executed more than once")
	}
}
