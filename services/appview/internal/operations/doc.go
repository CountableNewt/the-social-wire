// Package operations ports the parts of OperationsCore the standalone AppView
// runs: the SQLite operations schema, service heartbeats, the bounded
// telemetry buffer that rolls metrics, events and spans into SQLite, request
// tracing, the freshness probe, ingestion leader leases used by PDS read-state
// recovery, and the client performance telemetry route.
//
// Operations dashboards read the same SQLite tables, so DDL, row formats and
// timestamp strings match SQLiteOperationsStore exactly.
package operations
