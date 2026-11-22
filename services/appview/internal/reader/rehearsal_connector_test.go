package reader

import (
	"context"
	"database/sql/driver"
	"errors"

	turso "turso.tech/database/tursogo-serverless"
)

// The provider's read-only token rejects foreign_keys=ON. This test-only pool
// omits that write setup while preserving production's per-query Describe cost;
// server token policy independently prevents every persistent mutation.
type rehearsalDescribedConnector struct{ upstream *turso.Connector }

func (c rehearsalDescribedConnector) Driver() driver.Driver { return c.upstream.Driver() }

func (c rehearsalDescribedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connection, err := c.upstream.OpenConnection(ctx)
	if err != nil {
		return nil, err
	}
	return &rehearsalDescribedConnection{connection}, nil
}

type rehearsalDescribedConnection struct{ *turso.Connection }

func (c *rehearsalDescribedConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	description, err := c.Connection.DescribeContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if description == nil || !description.IsReadOnly {
		return nil, errors.New("rehearsal query is not server-described read-only")
	}
	return c.Connection.QueryContext(ctx, query, args)
}
