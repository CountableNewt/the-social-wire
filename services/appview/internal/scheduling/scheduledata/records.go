package scheduledata

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling"
)

const ScheduleCollection = "at.skej.schedule"

func (s *Store) WriteProtocolRecord(ctx context.Context, did, collection, rkey string, value json.RawMessage, now string) error {
	return writeProtocol(ctx, s.db.Writer(), did, collection, rkey, value, now)
}

func writeProtocol(ctx context.Context, db storeExecer, did, collection, rkey string, value json.RawMessage, now string) error {
	if !json.Valid(value) {
		return errors.New("scheduledata: invalid protocol record JSON")
	}
	_, err := db.ExecContext(ctx, `INSERT INTO pds_protocol_records(did,collection,rkey,record_json,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(did,collection,rkey) DO UPDATE SET record_json=excluded.record_json,updated_at=excluded.updated_at`,
		did, collection, rkey, string(value), now)
	return err
}

func (s *Store) ProtocolRecord(ctx context.Context, did, collection, rkey string) (json.RawMessage, error) {
	return queryRecord(s.db.Reader().QueryRowContext(ctx,
		"SELECT record_json FROM pds_protocol_records WHERE did=? AND collection=? AND rkey=?", did, collection, rkey))
}

func queryRecord(row rowScanner) (json.RawMessage, error) {
	var data []byte
	err := row.Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, errors.New("scheduledata: invalid stored record JSON")
	}
	return json.RawMessage(data), nil
}

func (s *Store) ListProtocolRecords(ctx context.Context, did, collection string) (map[string]json.RawMessage, error) {
	return listRecords(ctx, s.db.Reader(), "SELECT rkey,record_json FROM pds_protocol_records WHERE did=? AND collection=?", did, collection)
}

func listRecords(ctx context.Context, db jobsQueryer, query string, args ...any) (map[string]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var data []byte
		if err := rows.Scan(&key, &data); err != nil {
			return nil, err
		}
		if !json.Valid(data) {
			return nil, errors.New("scheduledata: invalid stored record JSON")
		}
		records[key] = json.RawMessage(data)
	}
	return records, rows.Err()
}

func (s *Store) DeleteProtocolRecord(ctx context.Context, did, collection, rkey string) error {
	_, err := s.db.Writer().ExecContext(ctx, "DELETE FROM pds_protocol_records WHERE did=? AND collection=? AND rkey=?", did, collection, rkey)
	return err
}

func (s *Store) WriteScheduleRecord(ctx context.Context, did, rkey string, record json.RawMessage, now string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if err := writeProtocol(ctx, tx, did, ScheduleCollection, rkey, record, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO pds_schedule_records(did,rkey,record_json,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(did,rkey) DO UPDATE SET record_json=excluded.record_json,updated_at=excluded.updated_at`,
			did, rkey, string(record), now)
		return err
	})
}

func (s *Store) ScheduleRecord(ctx context.Context, did, rkey string) (json.RawMessage, error) {
	return queryRecord(s.db.Reader().QueryRowContext(ctx, `SELECT record_json FROM pds_protocol_records
		WHERE did=?1 AND collection=?2 AND rkey=?3
		UNION ALL SELECT record_json FROM pds_schedule_records WHERE did=?1 AND rkey=?3 LIMIT 1`, did, ScheduleCollection, rkey))
}

func (s *Store) ListScheduleRecords(ctx context.Context, did string) (map[string]json.RawMessage, error) {
	return listRecords(ctx, s.db.Reader(), `SELECT rkey,record_json FROM pds_protocol_records WHERE did=?1 AND collection=?2
		UNION ALL SELECT legacy.rkey,legacy.record_json FROM pds_schedule_records legacy WHERE legacy.did=?1 AND NOT EXISTS(
			SELECT 1 FROM pds_protocol_records active WHERE active.did=legacy.did AND active.collection=?2 AND active.rkey=legacy.rkey)`,
		did, ScheduleCollection)
}

func (s *Store) DeleteScheduleRecord(ctx context.Context, did, rkey string) error {
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pds_protocol_records WHERE did=? AND collection=? AND rkey=?", did, ScheduleCollection, rkey); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM pds_schedule_records WHERE did=? AND rkey=?", did, rkey)
		return err
	})
}

type PublishedSchedule struct {
	DID, RKey string
	Record    json.RawMessage
}

func (s *Store) FindPublishedSchedule(ctx context.Context, scheduleURI string) (*PublishedSchedule, error) {
	at, ok := atproto.ParseRecordURI(scheduleURI)
	if !ok || at.Collection != ScheduleCollection {
		return nil, nil
	}
	data, err := s.ScheduleRecord(ctx, at.Repo, at.RKey)
	if err != nil || data == nil {
		return nil, err
	}
	var status struct {
		Status scheduling.ScheduleStatus `json:"status"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	if status.Status != scheduling.StatusPublished {
		return nil, nil
	}
	return &PublishedSchedule{at.Repo, at.RKey, data}, nil
}

func (s *Store) UpdateParentPublishedURI(ctx context.Context, did, rkey, parentPublishedURI, now string) error {
	_, err := s.db.Writer().ExecContext(ctx, "UPDATE scheduled_jobs SET parent_published_uri=?,updated_at=? WHERE did=? AND rkey=?",
		parentPublishedURI, now, did, rkey)
	return err
}

type AuditEvent struct {
	ID           string  `json:"id"`
	DID          string  `json:"did"`
	ScheduleRKey *string `json:"scheduleRkey,omitempty"`
	Action       string  `json:"action"`
	Message      string  `json:"message"`
	CreatedAt    string  `json:"createdAt"`
}

func (s *Store) InsertAuditEvent(ctx context.Context, did string, scheduleRKey *string, action, message, now string) error {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	id := encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
	_, err := s.db.Writer().ExecContext(ctx, "INSERT INTO audit_events(id,did,schedule_rkey,action,message,created_at) VALUES(?,?,?,?,?,?)",
		id, did, scheduleRKey, action, message, now)
	return err
}

func (s *Store) ListAuditEvents(ctx context.Context, did string, limit int) ([]AuditEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT id,did,schedule_rkey,action,message,created_at FROM audit_events
		WHERE did=? ORDER BY julianday(created_at) DESC,id LIMIT ?`, did, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var value AuditEvent
		if err := rows.Scan(&value.ID, &value.DID, &value.ScheduleRKey, &value.Action, &value.Message, &value.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, value)
	}
	return events, rows.Err()
}
