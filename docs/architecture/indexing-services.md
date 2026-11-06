# Replicated indexing and ingestion services

The active ingestion and indexing control plane uses three independent Swift
service classes. Web and iOS remain thin clients; Gateway and App View stay
separate services. Operations and dormant corpus-edge services are excluded from
the Swift migration tracked in [GitHub #782](https://github.com/Stygian-Tech/the-social-wire/issues/782).

| Service class | Package | Replicas | Active responsibilities |
| --- | --- | ---: | --- |
| Ingress Controller | `services/jetstream-ingest` | 2 | Jetstream V2 archive/live AppView intake, fenced leases, transactional inbox/checkpoints, bounded replay budgets |
| Projection Pool | `services/indexing-worker`, role `projection` | N | AppView durable-inbox projection using PostgreSQL claims and acknowledgements |
| Coordinator | `services/indexing-worker`, role `coordinator` | 2 | AppView RSS polling, signed repository reconciliation, backfill, retention, and recovery under fenced authority |

```text
Jetstream V2 archives / live tail
            │
            ▼
Ingress Controller ×2 ── fenced intake lease ── AppView durable inbox
            │
            ▼
Projection Pool ×N ── claim / project / acknowledge ── App View reads

Coordinator ×2 ── appview-coordinator lease ── RSS / backfill / TTL / recovery
```

## Isolation and compatibility

The Postgres inbox boundary, database schema, source generations, replay cursors,
and claim/ack semantics remain stable across the language migration. Only
`v2_authoritative` AppView ingestion is supported by the active workers. Tap,
legacy combined ingestion, enabled Wire/Finance/Sports workers, and standalone
retired worker executables must not be started. IaC explicitly holds excluded
feed modes off. Retained Go sources support parity checks and rollback review.

All active images build with Swift 6.4.2 and run the native `schema-ready` gate
before the service binary. Service manifests pin remote shared packages; local
SwiftPM mirrors never enter images. Each process joins cancellation and closes
owned clients, pools, and listeners during shutdown. Authority and diagnostic
Postgres pools remain separate from workload pools. The worker image also ships
the native `retry-reconciliation` operator tool with explicit scoped admission.

## Singleton fencing

Coordinator replicas use `operations_role_leases`, keyed by `(environment, role)`. Takeover increments a monotonic fencing token. Renew, release, and fenced commit entry require the exact owner and token, so a stale owner cannot regain authority after lease loss.

The active Coordinator uses `indexing.appview-coordinator`. A replica without
the lease remains a healthy standby while it can reach PostgreSQL and its lease
supervisor is running. The active owner must also pass component health checks.
Wire materialization remains disabled.

## Scaling signals

- Scale Ingress Controller only for availability. Fenced lane ownership keeps one active intake owner per source generation.
- Scale Projection Pool from actionable inbox age, pending rows, claim latency, and database connection headroom.
- Keep Coordinator at two replicas. Additional replicas add standby connections without increasing singleton throughput.
- Run snapshots as bounded jobs with a unique source generation and explicit cursor bounds. Never place snapshot mode under an always-restart supervisor.

## Deployment ownership

Railway Infrastructure as Code for the three long-running classes lives in
`/.railway/railway.ts`. Its stable `indexing-consolidation` partial owns only
Ingress Controller, Projection Pool, and Coordinator while compatibility
services remain on grandfathered `railway/*.json` configuration. The partial
selects an explicit Development or Production profile; applying or promoting it
remains an environment-scoped operator action.

Snapshots are temporary operator-created services, not persistent IaC
resources. They use the ingress Dockerfile, bounded cursor variables, restart
policy `NEVER`, and are removed after their completion evidence is captured.

Database Migrator remains the only schema owner. All three long-running service classes reference its Railway service ID so a successful migration precedes application startup. Environment cutover and rollback steps are in [the consolidation runbook](../runbooks/operations/indexing-service-consolidation.md).
