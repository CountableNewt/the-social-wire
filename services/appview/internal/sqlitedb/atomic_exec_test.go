package sqlitedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
)

type execProbe struct {
	begins, execs, commits, rollbacks atomic.Int64
	beginErr, execErr, commitErr      error
	cancel                            context.CancelFunc
	query                             string
	args                              []driver.NamedValue
}
type execProbeConnector struct{ p *execProbe }

func (c execProbeConnector) Connect(context.Context) (driver.Conn, error) { return c.p, nil }
func (c execProbeConnector) Driver() driver.Driver                        { return nil }
func (p *execProbe) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unexpected")
}
func (p *execProbe) Close() error { return nil }
func (p *execProbe) Begin() (driver.Tx, error) {
	return p.BeginTx(context.Background(), driver.TxOptions{})
}
func (p *execProbe) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	p.begins.Add(1)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return p, p.beginErr
}
func (p *execProbe) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	p.execs.Add(1)
	p.query = q
	p.args = a
	if p.cancel != nil {
		p.cancel()
		return nil, ctx.Err()
	}
	return execProbeResult{}, p.execErr
}
func (p *execProbe) Commit() error   { p.commits.Add(1); return p.commitErr }
func (p *execProbe) Rollback() error { p.rollbacks.Add(1); return nil }

type execProbeResult struct{}

func (execProbeResult) LastInsertId() (int64, error) { return 17, nil }
func (execProbeResult) RowsAffected() (int64, error) { return 3, nil }
func TestExecContextPreservesBindingsAndCommittedResult(t *testing.T) {
	for _, remote := range []bool{false, true} {
		p := &execProbe{}
		pool := sql.OpenDB(execProbeConnector{p})
		db := &DB{write: pool, read: pool, remote: remote}
		result, e := db.ExecContext(t.Context(), "mutation ?", "private-value")
		if e != nil {
			t.Fatal(e)
		}
		id, _ := result.LastInsertId()
		rows, _ := result.RowsAffected()
		if id != 17 || rows != 3 || p.execs.Load() != 1 || p.query != "mutation ?" || len(p.args) != 1 || p.args[0].Value != "private-value" {
			t.Fatal("lost mutation metadata or bindings")
		}
		expected := int64(0)
		if remote {
			expected = 1
		}
		if p.begins.Load() != expected || p.commits.Load() != expected || p.rollbacks.Load() != 0 {
			t.Fatal("incorrect transaction ownership")
		}
		pool.Close()
	}
}
func TestRemoteExecContextFailuresHaveNoResultOrReplay(t *testing.T) {
	original := errors.New("original failure")
	for _, phase := range []string{"begin", "execute", "commit", "canceled"} {
		t.Run(phase, func(t *testing.T) {
			p := &execProbe{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch phase {
			case "begin":
				p.beginErr = original
			case "execute":
				p.execErr = original
			case "commit":
				p.commitErr = original
			case "canceled":
				p.cancel = cancel
			}
			pool := sql.OpenDB(execProbeConnector{p})
			defer pool.Close()
			db := &DB{write: pool, read: pool, remote: true}
			result, e := db.ExecContext(ctx, "mutation ?", "private-value")
			if result != nil {
				t.Fatal("failed/uncertain mutation exposed result")
			}
			want := original
			if phase == "canceled" {
				want = context.Canceled
			}
			if !errors.Is(e, want) {
				t.Fatal("original failure lost")
			}
			if p.begins.Load() != 1 || p.execs.Load() > 1 || p.commits.Load() > 1 {
				t.Fatal("mutation replayed")
			}
			if phase == "execute" && p.rollbacks.Load() != 1 {
				t.Fatal("failed mutation not rolled back")
			}
			if phase == "commit" && (p.execs.Load() != 1 || p.commits.Load() != 1 || p.rollbacks.Load() != 0) {
				t.Fatal("uncertain commit retried or rolled back")
			}
		})
	}
}
