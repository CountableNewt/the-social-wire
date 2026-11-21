package operations

import (
	"context"
	"database/sql"
)

// FetchStreamState reads one ingestion source's state, or nil when absent or malformed.
func (s *Store) FetchStreamState(ctx context.Context, source string) (*StreamState, error) {
	var state *StreamState
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		var (
			row                                                                        StreamState
			connectedAt, lastDisconnectAt, lastReceivedEventAt, lastReceivedAt         sql.NullString
			lastCommittedEventAt, lastCommittedAt, queueObservedAt, transportHeartbeat sql.NullString
			lastIndexedMutation, disconnectReason, projection, validation              sql.NullString
			receivedCursor, committedCursor, overflow                                  sql.NullInt64
			capacity                                                                   sql.NullInt64
			heartbeatAt                                                                string
		)
		err := tx.QueryRowContext(ctx, `
SELECT environment, source, connection_state, connected_at, last_disconnect_at,
  last_disconnect_reason, last_received_cursor, last_received_event_at, last_received_at,
  last_committed_cursor, last_committed_event_at, last_committed_at, queue_depth,
  queue_capacity, queue_overflow_total, queue_observed_at, transport_heartbeat_at,
  last_indexed_mutation_at, projection_watermark, validation_watermark, heartbeat_at, version
FROM appview_ingestion_stream_state WHERE environment = ? AND source = ? LIMIT 1`,
			s.environment, source).Scan(&row.Environment, &row.Source, &row.ConnectionState,
			&connectedAt, &lastDisconnectAt, &disconnectReason, &receivedCursor, &lastReceivedEventAt,
			&lastReceivedAt, &committedCursor, &lastCommittedEventAt, &lastCommittedAt, &row.QueueDepth,
			&capacity, &overflow, &queueObservedAt, &transportHeartbeat, &lastIndexedMutation,
			&projection, &validation, &heartbeatAt, &row.Version)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		switch row.ConnectionState {
		case "connected", "disconnected", "reconnecting", "unknown":
		default:
			return nil
		}
		var ok bool
		if row.HeartbeatAt, ok = parseStoreTime(heartbeatAt); !ok {
			return nil
		}
		row.ConnectedAt = parseNullTime(connectedAt)
		row.LastDisconnectAt = parseNullTime(lastDisconnectAt)
		row.LastReceivedEventAt = parseNullTime(lastReceivedEventAt)
		row.LastReceivedAt = parseNullTime(lastReceivedAt)
		row.LastCommittedEventAt = parseNullTime(lastCommittedEventAt)
		row.LastCommittedAt = parseNullTime(lastCommittedAt)
		row.QueueObservedAt = parseNullTime(queueObservedAt)
		row.TransportHeartbeatAt = parseNullTime(transportHeartbeat)
		row.LastIndexedMutationAt = parseNullTime(lastIndexedMutation)
		row.LastDisconnectReason = nullString(disconnectReason)
		row.ProjectionWatermark = nullString(projection)
		row.ValidationWatermark = nullString(validation)
		row.LastReceivedCursor = nullInt64(receivedCursor)
		row.LastCommittedCursor = nullInt64(committedCursor)
		row.QueueOverflowTotal = nullInt64(overflow)
		if capacity.Valid {
			value := int(capacity.Int64)
			row.QueueCapacity = &value
		}
		state = &row
		return nil
	})
	return state, err
}

// HasConfirmedGap reports whether any of the newest limit (1...250) gaps,
// ordered as listGaps(limit:) orders them, is confirmed.
func (s *Store) HasConfirmedGap(ctx context.Context, limit int) (bool, error) {
	limit = max(1, min(limit, 250))
	found := false
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT status, detected_at, updated_at FROM appview_ingestion_gaps
WHERE environment = ? ORDER BY detected_at DESC, id DESC LIMIT ?`, s.environment, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var status, detectedAt, updatedAt string
			if err := rows.Scan(&status, &detectedAt, &updatedAt); err != nil {
				return err
			}
			_, detectedOK := parseStoreTime(detectedAt)
			_, updatedOK := parseStoreTime(updatedAt)
			if status == "confirmed" && detectedOK && updatedOK {
				found = true
			}
		}
		return rows.Err()
	})
	return found, err
}

func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
