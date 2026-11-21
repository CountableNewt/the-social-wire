package operations

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

// Store is the SQLite operations store (SQLiteOperationsStore), scoped to one
// environment. Call Migrate before using it.
type Store struct {
	db          *sqlitedb.DB
	environment string
	logger      *slog.Logger

	mu     sync.Mutex
	fences map[string]leaseFence
}

type leaseFence struct {
	ownerID      string
	fencingToken int64
	count        int
}

// NewStore returns a store over db for environment, the value RequireEnvironment returned.
func NewStore(db *sqlitedb.DB, environment string, logger *slog.Logger) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{db: db, environment: environment, logger: logger, fences: map[string]leaseFence{}}
}

// Environment is the store's environment scope.
func (s *Store) Environment() string { return s.environment }

// Ping runs SELECT 1 on a reader.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Read(ctx, func(tx *sql.Tx) error {
		var one int
		return tx.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	})
}

// UpsertServiceState writes one heartbeat row, keeping the original started_at.
func (s *Store) UpsertServiceState(ctx context.Context, state ServiceState) error {
	if state.Environment != s.environment {
		return &EnvironmentMismatchError{Expected: s.environment, Actual: state.Environment}
	}
	dependencies := swiftJSON(state.DependencyState)
	return s.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO operations_service_state
  (service, environment, instance_id, liveness, readiness, freshness, completeness,
   dependency_state, version, started_at, heartbeat_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (service, environment, instance_id) DO UPDATE SET
  liveness = excluded.liveness, readiness = excluded.readiness,
  freshness = excluded.freshness, completeness = excluded.completeness,
  dependency_state = excluded.dependency_state, version = excluded.version,
  heartbeat_at = excluded.heartbeat_at`,
			state.Service, state.Environment, state.InstanceID, string(state.Liveness),
			string(state.Readiness), string(state.Freshness), string(state.Completeness),
			dependencies, state.Version, storeTime(state.StartedAt), storeTime(state.HeartbeatAt))
		return err
	})
}

// ListServiceStates returns heartbeats from the last 120 seconds, annotating
// coordinator-appview rows with their validated coordinator authority.
func (s *Store) ListServiceStates(ctx context.Context) ([]ServiceState, error) {
	now := time.Now()
	var states []ServiceState
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
SELECT state.service, state.environment, state.instance_id, state.liveness, state.readiness,
  state.freshness, state.completeness, state.dependency_state, state.version,
  state.started_at, state.heartbeat_at,
  CASE WHEN lease.owner_id = json_extract(state.dependency_state, '$.coordinator_owner_id')
    AND CAST(lease.fencing_token AS TEXT) = json_extract(state.dependency_state, '$.coordinator_fencing_token')
    AND lease.role = json_extract(state.dependency_state, '$.coordinator_role')
    AND lease.released_at IS NULL AND lease.lease_expires_at > ?
  THEN 'active' ELSE 'inactive' END AS coordinator_authority
FROM operations_service_state state
LEFT JOIN operations_role_leases lease ON lease.environment = state.environment
  AND lease.role = 'indexing.appview-coordinator'
WHERE state.environment = ? AND state.heartbeat_at > ?
ORDER BY state.service, state.heartbeat_at DESC`,
			storeTime(now), s.environment, storeTime(now.Add(-120*time.Second)))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				state                                                      ServiceState
				liveness, readiness, freshness, completeness, dependencies string
				version                                                    sql.NullString
				startedAt, heartbeatAt, authority                          string
			)
			if err := rows.Scan(&state.Service, &state.Environment, &state.InstanceID, &liveness,
				&readiness, &freshness, &completeness, &dependencies, &version, &startedAt,
				&heartbeatAt, &authority); err != nil {
				return err
			}
			var ok [6]bool
			state.Liveness, ok[0] = validHealthState(liveness)
			state.Readiness, ok[1] = validHealthState(readiness)
			state.Freshness, ok[2] = validHealthState(freshness)
			state.Completeness, ok[3] = validHealthState(completeness)
			state.StartedAt, ok[4] = parseStoreTime(startedAt)
			state.HeartbeatAt, ok[5] = parseStoreTime(heartbeatAt)
			if ok != [6]bool{true, true, true, true, true, true} {
				continue
			}
			if version.Valid {
				state.Version = &version.String
			}
			state.DependencyState = decodeStringMap(dependencies)
			if state.Service == "coordinator-appview" {
				state.DependencyState["coordinator_authority"] = authority
			}
			states = append(states, state)
		}
		return rows.Err()
	})
	return states, err
}

func boundedLeaseName(name string) string { return prefixRunes(name, 128) }
