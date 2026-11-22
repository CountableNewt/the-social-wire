package sqlitedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	turso "turso.tech/database/tursogo-serverless"
)

type tursoConnector struct {
	connector *turso.Connector
	maxIdle   time.Duration
	now       func() time.Time
}

func (c *tursoConnector) Driver() driver.Driver { return c.connector.Driver() }

func (c *tursoConnector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.connector.OpenConnection(ctx)
	if err != nil {
		return nil, remoteError(err)
	}
	fail := func(err error) (driver.Conn, error) { _ = raw.Close(); return nil, remoteError(err) }
	// A fixed two-statement sequence uses the official driver pipeline path,
	// avoiding cursor-stream EOF delays observed during connection setup.
	// Verification below remains independent; no user mutation is replayed.
	if _, err := raw.ExecContext(ctx, "PRAGMA foreign_keys=ON; SELECT 1", nil); err != nil {
		return fail(err)
	}
	rows, err := raw.QueryContext(ctx, "PRAGMA foreign_keys", nil)
	if err != nil {
		return fail(err)
	}
	values := make([]driver.Value, 1)
	err = rows.Next(values)
	closeErr := rows.Close()
	if err != nil || closeErr != nil || values[0] != int64(1) {
		return fail(errors.New("Turso foreign key enforcement unavailable"))
	}
	connection := &tursoConnection{remoteSession: raw, maxIdle: c.maxIdle, now: c.now}
	connection.markActivity()
	return connection, nil
}

// The upstream BeginTx currently ignores TxOptions and uses deferred BEGIN.
// Adapt it here so every database/sql caller (including direct BeginTx users)
// keeps immediate write admission and server-described read-only snapshots.
type remoteSession interface {
	driver.Conn
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.Validator
	DescribeContext(context.Context, string) (*turso.Description, error)
}

type tursoConnection struct {
	remoteSession
	readOnly     bool
	broken       bool
	maxIdle      time.Duration
	now          func() time.Time
	lastActivity time.Time
}

func (c *tursoConnection) IsValid() bool { return !c.broken && c.remoteSession.IsValid() }
func (c *tursoConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *tursoConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options.Isolation != driver.IsolationLevel(sql.LevelDefault) {
		return nil, errors.New("Turso transaction isolation option is unsupported")
	}
	query := "BEGIN IMMEDIATE"
	if options.ReadOnly {
		query = "BEGIN"
	}
	defer c.markActivity()
	// Control statements use the same bounded pipeline path as initialization.
	// The official driver refreshes autocommit state after the sequence.
	if _, err := c.remoteSession.ExecContext(ctx, query+"; SELECT 1", nil); err != nil {
		return nil, remoteError(err)
	}
	c.readOnly = options.ReadOnly
	return &tursoTransaction{connection: c, ctx: ctx}, nil
}

func (c *tursoConnection) allow(ctx context.Context, query string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !c.readOnly {
		return nil
	}
	// Describe is authoritative for one statement only. Conservatively reject
	// scripts and transaction-control SQL before asking the server; unlike write
	// migrations, a read-only callback has no reason to execute a SQL script.
	words := strings.Fields(query)
	if strings.Contains(query, ";") || len(words) == 0 {
		return errors.New("read-only Turso transaction requires one query statement")
	}
	switch strings.ToUpper(words[0]) {
	case "SELECT", "WITH", "EXPLAIN":
	default:
		return errors.New("read-only Turso transaction requires one query statement")
	}
	description, err := c.remoteSession.DescribeContext(ctx, query)
	c.markActivity()
	if err != nil {
		return remoteError(err)
	}
	if description == nil || !description.IsReadOnly {
		return errors.New("write attempted in a read-only Turso transaction")
	}
	return nil
}

func (c *tursoConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.allow(ctx, query); err != nil {
		return nil, err
	}
	result, err := c.remoteSession.ExecContext(ctx, query, args)
	c.markActivity()
	return result, remoteError(err)
}
func (c *tursoConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.allow(ctx, query); err != nil {
		return nil, err
	}
	// The official driver buffers remote rows before returning. Iteration and
	// Rows.Close perform no network activity, so they must not renew backend idle time.
	rows, err := c.remoteSession.QueryContext(ctx, query, args)
	c.markActivity()
	return rows, remoteError(err)
}
func (c *tursoConnection) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *tursoConnection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.allow(ctx, query); err != nil {
		return nil, err
	}
	statement, err := c.remoteSession.PrepareContext(ctx, query)
	c.markActivity()
	if err != nil {
		return nil, remoteError(err)
	}
	return &tursoStatement{Stmt: statement, connection: c, query: query}, nil
}
func (c *tursoConnection) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	defer c.markActivity()
	return remoteError(c.remoteSession.Ping(ctx))
}
func (c *tursoConnection) Close() error { return remoteError(c.remoteSession.Close()) }

type tursoStatement struct {
	driver.Stmt
	connection *tursoConnection
	query      string
}

func namedValues(values []driver.Value) []driver.NamedValue {
	result := make([]driver.NamedValue, len(values))
	for index, value := range values {
		result[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return result
}
func (s *tursoStatement) Exec(values []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(values))
}
func (s *tursoStatement) Query(values []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(values))
}
func (s *tursoStatement) ExecContext(ctx context.Context, values []driver.NamedValue) (driver.Result, error) {
	return s.connection.ExecContext(ctx, s.query, values)
}
func (s *tursoStatement) QueryContext(ctx context.Context, values []driver.NamedValue) (driver.Rows, error) {
	return s.connection.QueryContext(ctx, s.query, values)
}

type tursoTransaction struct {
	connection *tursoConnection
	ctx        context.Context
}

func (t *tursoTransaction) Commit() error {
	_, err := t.connection.remoteSession.ExecContext(t.ctx, "COMMIT; SELECT 1", nil)
	t.connection.markActivity()
	t.connection.readOnly = false
	if err != nil {
		// The commit outcome is unknown. Invalidate, do not replay the write or
		// return ErrBadConn (which can instruct database/sql to retry it).
		t.connection.broken = true
		_ = t.connection.remoteSession.Close()
	}
	return remoteError(err)
}
func (t *tursoTransaction) Rollback() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := t.connection.remoteSession.ExecContext(ctx, "ROLLBACK; SELECT 1", nil)
	t.connection.markActivity()
	t.connection.readOnly = false
	if err != nil {
		t.connection.broken = true
		_ = t.connection.remoteSession.Close()
	}
	return remoteError(err)
}
