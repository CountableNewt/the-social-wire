package reader

import (
	"context"
	"database/sql"
)

type readerQueryCounter struct {
	*sql.Tx
	count int
}

func (c *readerQueryCounter) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	c.count++
	return c.Tx.QueryContext(ctx, query, args...)
}

func (c *readerQueryCounter) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c.count++
	return c.Tx.QueryRowContext(ctx, query, args...)
}
