# Standalone Reader Backend

The current hosted deployment has two Railway services: Next.js Web and one Backend container.
The Backend runs the Go public API, SQLite-backed reader and draft stores,
RSS polling, retention and the embedded scheduler.

PDS records remain authoritative for user-owned protocol data. SQLite owns the
local projections and private unpublished state. The existing encrypted attachment
bucket remains in use. No Postgres, Redis, separate Gateway,
or Coordinator connection is used by standalone mode. The retired Swift distributed backend sources are removed. Retained deployment images and backups provide historical recovery material.

## Images and Configuration

Build from the repository root:

```sh
docker build -f apps/web/Dockerfile -t social-wire-web .
docker build -f services/appview/Dockerfile.standalone -t social-wire-backend .
(cd services/appview && go test -vet=all -count=1 ./...)
bash scripts/ci/verify-runtime-images.sh appview
```

Web image public build arguments must match the destination environment; see its
Dockerfile. Server-only Web credentials stay in Railway,
never in image build arguments.

Select `/railway/web-standalone.json` on the existing Web service to run the same
final Node image verified by CI. Preserve its public build arguments and runtime-only
L@tr credentials; the legacy Web config remains available for rollback.

Select `/railway/appview-standalone.json` on the existing Gateway service and
rename that service to AppView after the cutover. Reusing its service identity
preserves its public domains,
including Development clients using the Railway-generated Gateway hostname.
Add one persistent US-West volume mounted at `/data`, with one replica. Set
`BACKEND_MODE=standalone`, `SQLITE_DB_PATH=/data/social-wire.sqlite`, canonical
`APP_ENV=dev|prod`, and keep `PORT` aligned with the current domain target port
(`8080` for the reused Gateway services).
The standalone config selects `Dockerfile.standalone`, requires the `/data`
mount, fixes one replica in `sfo`, disables sleep and deployment overlap, and
clears the old start/pre-deploy commands. The image also refuses startup or
import when `/data` is only an ephemeral directory. Use the image's standalone
entrypoint and remove the old migrator-reference variable on this service.
The retired distributed Dockerfiles, service configs and `.railway/` graph are removed; do not recreate those services from old instructions.

Combine the current Gateway and App View settings.
Preserve the exact existing values of:

- Public OAuth origins, CORS policy, issuer/JWKS policy and
  `PDS_ATTESTATION_RECEIPT_SECRET`.
- L@tr client credentials, GIF key and Pro entitlement settings.
- `SOCIAL_DRAFT_STORAGE_KEY`, draft S3 settings and recovery keys.
- `SCHEDULING_STORAGE_KEY` and the scheduling OAuth public origin.
  Keep `/data/scheduling.sqlite` on the same persistent volume.
- `GATEWAY_APPVIEW_INTERNAL_SECRET`: retained as the analytics pseudonym HMAC
  key, despite the removed internal network hop.

Global ingestion and its feature switch are removed. The Wire,
Your Circle, Finance and Sports were removed, so their `*_FEED_MODE` variables are
unused. Preserve the current provider-backed
content discovery and retained feature settings. Stale Redis/Postgres variables
are unused by this mode; remove them after the replacement is accepted.
Podcasts were retired; `PODCAST_*` variables are unused, but repoint any
`SOCIAL_DRAFT_S3_*` references to the bucket before deleting them.

## Switching an Existing SQLite Deployment to Go

Back up the AppView and scheduling SQLite files before replacing the Swift image.
Rehearse initialization against a copy of the volume with the same feature
settings. Compare database integrity, representative reader projections,
private drafts and the scheduled queue. Validate authenticated flows in
Development before promoting the same revision to Production.

## Development-First Releases

The Postgres-to-SQLite cutover is historical. Its importer and Postgres client are removed from the current runtime; recover an old import procedure from its exact Git revision only if needed. Keep historical databases and backup receipts separate from current deployment configuration.

Before deploying schema cleanup, back up AppView and scheduling databases, rehearse initialization against copies and preserve encrypted object buckets and keys. Validate authenticated reader, draft, scheduling and read-state workflows in Development before promoting the same source to Production. Health checks alone are not acceptance.

The #963 timestamp-portability and multi-instance coordination changes must land before Turso and Vercel. Until then, preserve the single-replica persistent-volume deployment. Hosted service, volume and database retirement is a separate operator action.

## Timestamp Portability

Go migrations backfill integer microsecond comparison keys for content created/indexed timestamps, read floors, unread overrides and PDS boundaries. They retain the original strings and existing feed/cursor ordering. Conversion uses integer arithmetic, rounds sub-microsecond values to the nearest microsecond with ties away from the epoch, and rejects malformed historical values.

Old content projection triggers are removed before backfill so the migration cannot rewrite read marks or advance authority revisions. Reader migration installs replacement triggers with stored-integer comparisons. Startup reports any remaining schema object that depends on the retired custom timestamp function or collation.

Before release, rehearse against copies of both environment databases, compare read-state projections and pagination, and confirm the migrated files open without custom extensions. This source work does not execute a hosted migration.

## Durable Maintenance Jobs

The discovery queue persists normalized publication scopes, deduplicates requests, and uses owner-fenced claims with expiry and bounded retry/backoff. Scheduling creation uses an actor lease; OAuth refresh additionally records a hashed token intent before exchange so an uncertain or consumed refresh token cannot be retried by another instance. An uncertain refresh may require reconnecting the scheduling account.

`RUNTIME_MODE=server` and `JOBS_RUNNER_MODE=internal` are the local/Railway defaults. Server timers call the same bounded, leased actions exposed by the manual POST endpoints and dedicated Cron GET adapters:

| Job Suffix | Internal Interval | Vercel Cron | Execution Bound |
|------------|-------------------|-------------|-----------------|
| `scheduling` | 30 seconds | Every minute | 150 seconds |
| `drafts-cleanup` | 15 minutes | Every 15 minutes | 45 seconds |
| `discovery` | 1 second between waves | Every minute | 30 seconds, up to four claims |
| `ttl-cleanup` | 1 hour | Hourly | 45 seconds, up to ten batches |
| `rss-poll` | 30 minutes, configurable | Every 30 minutes | 90 seconds |
| `analytics-cleanup` | 24 hours | Daily at 00:00 UTC | 45 seconds |
| `author-history` | 1 second between waves | Every minute | 90 seconds, up to two claims |
| `read-state-recovery` | 1 second between waves | Every minute | 60 seconds, up to two claims |

Manual `POST /internal/jobs/<suffix>` calls require `Authorization: Bearer <JOBS_SECRET>`. Dedicated `GET /internal/cron/<suffix>` calls require a distinct `CRON_SECRET`; Vercel Cron supplies that Bearer header. Cron credentials cannot authorize manual POST jobs, and viewer credentials authorize neither adapter. Use unique server-only secrets of 32–512 non-whitespace characters; never put them in query strings, public Web build arguments, or native clients. An active lease makes duplicate invocations return without running the action again.

`JOBS_RUNNER_MODE=external` disables all eight timers and the process heartbeat and requires `JOBS_SECRET`. `RUNTIME_MODE=serverless` additionally requires remote Turso and a valid distinct `CRON_SECRET`; any Vercel runtime must explicitly select serverless. Serverless starts no maintenance, history, heartbeat or telemetry export loops. Request telemetry flushes synchronously within a bounded request scope. Author history and public PDS read-state recovery persist their work and checkpoints for later invocations. Shared-store concurrency and request lifecycle acceptance remain unverified.

All queue, lease, endpoint and concurrency regression tests added during #963 source preparation still require execution. No hosted two-replica proof is claimed.

## Turso Remote Backend

The selected remote target is the newer **Turso Database engine**, using the official pure-Go `turso.tech/database/tursogo-serverless` driver. This updates #963's original classic-libSQL target. Local SQLite remains the default and the current Railway deployment is unchanged until cutover.

Set `DATABASE_BACKEND=turso` only with all four server-only values:

- `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN` for AppView.
- `SCHEDULING_TURSO_DATABASE_URL` and `SCHEDULING_TURSO_AUTH_TOKEN` for a distinct scheduling database.

Use bare TLS `turso://` or `https://` endpoints without credentials, query strings or fragments. Tokens stay separate from URLs and Web/iOS configuration. Preserve the exact scheduling encryption key. Partial remote configuration fails; it never falls back to a local file.

Remote connections use one pool without local WAL/read-pool pragmas. The connector retains sessions, verifies foreign-key enforcement, uses immediate write transactions and checks read-only transactions against server statement descriptions. Unknown mutation or commit outcomes are not automatically replayed. Engine/session/transaction compatibility still requires execution against a disposable Turso database.

Remote serving performs schema readiness and encrypted-vault checks only. Run `AppView --initialize-only` explicitly before serving. The optional `railway/appview-turso.json` config runs it as a pre-deploy command and has no volume requirement; the existing standalone config remains the local deployment target. Both database readiness markers must be current before remote serving succeeds. Use a maintenance window for import/schema changes; do not let old and new writers run together.

Before any import, create verified offline snapshots using `sqlite-backup`. On scheduling, complete legacy plaintext-vault migration and secure cleanup locally first. Remote mode refuses plaintext, a pending cleanup marker, or a mismatched encryption key; unsupported remote secure-delete/checkpoint/VACUUM operations are not silently skipped as a substitute.

The read-only preflight command records schema hashes, table counts and migration IDs without exporting rows or secrets:

```sh
database-preflight --kind appview --snapshot /absolute/offline-appview.sqlite
database-preflight --kind scheduling --snapshot /absolute/offline-scheduling.sqlite
```

Preflight rejects symlinks, active sidecars, integrity/foreign-key failures, residual timestamp extensions and unsafe scheduling vault state. It does not prove engine compatibility or import data. `sqlite-backup` refuses remote mode, preventing an accidental backup of a stale local file. Establish and rehearse Turso backup/restore before hosted cutover.

Development and Production each need separate AppView and scheduling databases. Database creation, import, restore rehearsal, actual billed-row measurements, cold/warm authenticated route latency and plan selection remain outstanding. Snapshot row counts are not billed rows read. Accept Development before Production; keep Railway recovery material until the planned stability window completes.

## Vercel Source Configuration

Create separate frontend and backend projects with Root Directory `apps/web` and `services/appview`, respectively. The backend config uses **Vercel Services (Beta)** with one Go service rooted at `.` and explicit `cmd/appview/main.go` entrypoint. Its build script compiles the entire package to `VERCEL_OUTPUT_FILE`; catch-all ingress preserves API paths. The Function has a 300-second limit in `sfo1`, and the eight Cron GET schedules above are declared in `services/appview/vercel.json`. Vercel Cron runs on production-target deployments, so a Development project must use `dev` as its production branch. Keep the Production project on `main`; accept Development before promoting the same source.

The backend config selects `RUNTIME_MODE=serverless`, `DATABASE_BACKEND=turso` and `JOBS_RUNNER_MODE=external`. Supply existing OAuth, draft, scheduling and telemetry settings, both distinct Turso database/token pairs, `JOBS_SECRET` and a different `CRON_SECRET` through backend project variables. Keep frontend credentials and public origins scoped to the frontend project. Do not copy backend secrets into frontend variables.

Serving performs readiness checks without startup DDL. The supported Services schema has no `preDeployCommand`; instead `scripts/vercel-build.sh` has an explicit final build step for release initialization. It runs the compiled `--initialize-only` command only when `DEPLOY_SCHEMA_INITIALIZATION=true`, `VERCEL_ENV=production`, and the pair `APP_ENV:VERCEL_GIT_COMMIT_REF` is `dev:dev` or `prod:main`. An explicitly requested initialization with any other environment or branch fails the build. Leave the opt-in disabled unless schema initialization is authorized for that release. Failure prevents activation; a successful build does not prove engine compatibility or authenticated acceptance.

Preview builds never initialize shared Development or Production stores. Give previews their own databases and initialize those explicitly outside the build; absent current readiness markers, the remote runtime cannot become ready. Avoid sharing private preview writes with either release environment.

Project provisioning alone does not establish deployment or database compatibility. Keep the Railway single-replica volume deployment and recovery material until remote compatibility, lifecycle, latency and authenticated workflows pass cutover checks. Track environment-specific provisioning, snapshot receipts, deployment IDs and acceptance evidence in issue #963.

## Backups and Rollback

Enable Railway volume backups. For a live consistent SQLite snapshot, run
`sqlite-backup /data/<unique-name>.sqlite`; the Go command uses SQLite online pager backup to include committed WAL data, verifies integrity and foreign keys, and refuses to overwrite a destination. It reserves an owner-only output file and cleans up only its own file on failure.
Copy verified snapshots off the source volume and retain the matching object
buckets and encryption keys. Never copy only the live main SQLite file.

Restore only while the Backend is stopped. Verify the snapshot,
restore its owner-only file permissions, and start one Backend replica. A
rollback to the old Postgres system after new private writes requires a reverse
migration or explicit reconciliation; swapping the image alone would hide
those writes. Prefer rolling the standalone image back while preserving its
SQLite volume. Retained Postgres is the pre-cutover recovery point.

GitHub remains the canonical source. GitHub Actions verifies both packaged runtime
images and the Development-first promotion guard; Railway builds and deploys the
protected `dev` and `main` branches using the selected standalone service configs.

Private draft attachments retain the 100MiB per-file limit on Functions by uploading sequential 3MiB parts through `app.thesocialwire.socialDraft.uploadAttachmentChunk`, followed by `completeAttachmentUpload`. The existing full-upload endpoint remains available for local callers. Each part requires the reserved asset UUID, zero-based part header, and exact known byte length; the final part contains the remainder (at most 34 parts). The server reserves encrypted immutable metadata and a random private object key before uploading an encrypted temporary part. Identical retries reuse that reservation and ciphertext; changed bytes return Conflict. Completion authenticates every part and verifies the reserved whole-file SHA-256 and content type before writing the existing encrypted attachment format. Temporary objects expire with the 24-hour reservation or retire after completion and are removed by drafts cleanup. Database owner leases fence mutations, completion, and deletion across replicas; two whole-file transfers bound temporary spool storage. Downloads authenticate and verify the entire attachment before emitting flushed 64KiB response chunks without Content-Length. These source paths still require deferred shared-store and hosted acceptance.

The selected Vercel destination for encrypted attachments is a private Blob store per environment. Set `SOCIAL_DRAFT_STORAGE_BACKEND=vercel-blob` and server-only `BLOB_READ_WRITE_TOKEN`, preserving `SOCIAL_DRAFT_STORAGE_KEY`. The adapter retains existing random object keys and server ciphertext, authenticates private reads, rejects redirects and bounds response bodies. Railway's S3 backend remains the default until every source object has been copied and destination ciphertext length and SHA-256 verified. Database snapshots, object copies and their existing encryption keys form one recovery set; do not retire a source bucket based on database migration alone.

After freezing source writers and cleanup, run `go run ./cmd/draft-object-migrate --receipt /absolute/private-receipt.json` from `services/appview`, with existing `SOCIAL_DRAFT_S3_*` source settings, destination `BLOB_READ_WRITE_TOKEN`, and explicit `SOCIAL_DRAFT_MIGRATION_SOURCE=s3` / `SOCIAL_DRAFT_MIGRATION_DESTINATION=vercel-blob` / `SOCIAL_DRAFT_MIGRATION_WRITERS_FROZEN=true`. The command copies ciphertext without decryption, conditionally creates missing destination keys, verifies byte length and SHA-256, refuses differing existing objects and binds its resumable owner-only receipt to both storage identities. A second source inventory detects drift; keep writers frozen until database and object cutover finish. This command never deletes source objects.
