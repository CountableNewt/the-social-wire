package sqlitedb

import (
	"context"
	"database/sql/driver"
	"time"
)

const retainedSessionMaxIdle = 15 * time.Second

var _ driver.SessionResetter = (*tursoConnection)(nil)

func (c *tursoConnection) clockNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *tursoConnection) markActivity() { c.lastActivity = c.clockNow() }

// ResetSession is called by database/sql before reusing a checked-in connection.
// ErrBadConn is safe only here: no statement from this checkout has been sent.
// Expiry during SQL remains an ordinary failure and must never replay a mutation.
func (c *tursoConnection) ResetSession(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	maxIdle := c.maxIdle
	if maxIdle <= 0 {
		maxIdle = retainedSessionMaxIdle
	}
	if !c.IsValid() || (!c.lastActivity.IsZero() && c.clockNow().Sub(c.lastActivity) > maxIdle) {
		return driver.ErrBadConn
	}
	return nil
}
