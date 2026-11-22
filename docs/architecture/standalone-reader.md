# Go AppView Runtime and Hosting

The current hosting target is Next.js Web and Go AppView on Vercel, with US Turso databases and private Blob storage. Issue #963 records environment-specific cutover and acceptance evidence. The old Railway writer remains frozen while public cutover checks and separate operator infrastructure retirement finish. Removing deployment configuration from source does not delete hosted services, volumes, buckets or backups.

PDS records remain authoritative for user-owned protocol data. AppView owns reader projections, private unpublished state, analytics and embedded scheduling. No retired Swift backend, global firehose, Postgres, Redis, separate Gateway or Coordinator is required. Web and Apple remain thin consumers of the existing API domains and authenticated unbuffered NDJSON bootstrap.

## Two Vercel Projects

| Project root | Production | Development |
| --- | --- | --- |
| `apps/web` | `main`, Production target | `dev` branch Preview |
| `services/appview` | `main`, Production target | `dev` branch Preview |

Development uses `https://testing.thesocialwire.app` and `https://api.testing.thesocialwire.app`; Production uses `https://thesocialwire.app` and `https://api.thesocialwire.app`. Preserve these stable OAuth identities. Configure branch-specific Preview variables, domains and credentials for `dev`; do not create separate Development projects or let arbitrary previews share either release environment's private data.

The Web config installs the root Bun workspace with the frozen lockfile and builds Next.js. Keep source files outside the Root Directory enabled for workspace dependencies. Backend Services Beta has six independently routed Go Functions: reader, publishing, scheduling, analytics, integrations and maintenance. Shared wiring lives in `internal/appviewruntime`; the allowlisted build script compiles each `cmd/<surface>` package. Functions have 300-second limits in `sfo1` and preserve existing route paths.

Native CLI deployments render the pure config helper into a private archive's sole `vercel.json`; `--local-config` does not parse TypeScript. Checked-in branch allowlists permit `main` and `dev`. GitHub remains the canonical source; readonly required CI and the Development-first parity gate precede release.

## Environment and Admission

Hosted AppView explicitly selects `BACKEND_MODE=standalone`, `RUNTIME_MODE=serverless`, `DATABASE_BACKEND=turso` and `JOBS_RUNNER_MODE=external`. Set canonical `APP_ENV=dev|prod` for the matching branch/target. Local server mode remains available without Vercel.

Keep credentials only on the owning project/target. Web's server-only L@tr settings belong to Web; backend database, scheduling, draft and job secrets belong to AppView. No backend tokens or encryption keys belong in public build variables or native clients.

Set `OAUTH_PUBLIC_ORIGIN` to the stable **API** origin. Set `CORS_ALLOWED_ORIGINS` explicitly to **frontend** origins:

- Development: `https://testing.thesocialwire.app`.
- Production: `https://thesocialwire.app,https://www.thesocialwire.app`.

The API OAuth origin is not a frontend CORS allowlist. Verify preflight and authenticated browser requests from the real Web domain; a healthy `/readyz` does not prove frontend admission. Keep OAuth issuer/DID binding, key-bound DPoP and fail-closed signature verification intact. Metadata alone does not authorize an arbitrary Preview origin.

Scheduling is always enabled. Preserve the exact existing `SCHEDULING_STORAGE_KEY` and `SCHEDULING_PUBLIC_ORIGIN`; do not generate a replacement key for imported encrypted sessions. `GATEWAY_APPVIEW_INTERNAL_SECRET` remains the analytics pseudonym HMAC key despite the retired internal network hop. Retired feature/ingestion flags and old `EMBEDDED_SCHEDULING_*` aliases are not active configuration.

## Turso Stores and Initialization

The selected remote engine is the newer Turso Database engine with the official pure-Go `turso.tech/database/tursogo-serverless` driver, replacing #963's original classic-libSQL target. Development and Production each have distinct AppView and scheduling databases in the United States. Supply both server-only endpoint/token pairs:

- `TURSO_DATABASE_URL` and `TURSO_AUTH_TOKEN`.
- `SCHEDULING_TURSO_DATABASE_URL` and `SCHEDULING_TURSO_AUTH_TOKEN`.

Use bare TLS `turso://` or `https://` endpoints without embedded credentials, query strings or fragments. Partial remote configuration fails and never falls back to an ephemeral local SQLite file. Remote sessions enforce foreign keys, immediate write transactions and described read-only statements. Unknown mutation/commit outcomes are not automatically replayed. Schema checks reject residual private timestamp extensions; stored integer microseconds preserve timestamp portability while API strings and cursor precision remain unchanged.

Remote serving verifies schema markers and encrypted-vault compatibility only; it never applies startup DDL. Run `AppView --initialize-only` explicitly against the intended two stores before serving. Complete plaintext scheduling-vault migration and secure cleanup locally before import, preserving its encryption key.

**Development is a Preview target:** initialize its databases explicitly outside builds with Development-only credentials. Preview builds never initialize shared release databases. Other previews require isolated stores and explicit initialization, or remain unavailable when readiness markers are missing.

The optional build initialization guard remains restrictive. Only a maintenance Function build with `DEPLOY_SCHEMA_INITIALIZATION=true`, `VERCEL_ENV=production`, and matching `APP_ENV:VERCEL_GIT_COMMIT_REF` of `dev:dev` or `prod:main` can run initialization after compilation. Ordinary releases leave the opt-in disabled. Do not weaken this guard to initialize Development Preview during a build.

Use a maintenance window for store import/schema changes and never run old/new writers together. Check engine compatibility, shared-store leases, authenticated reader/draft/scheduling behavior and recovery in Development before promoting the same source to Production. Health probes alone are not acceptance.

## Maintenance and Scheduling

All eight actions use durable leases and bounded runs. Manual `POST /internal/jobs/<name>` requires server-only `JOBS_SECRET`. The consolidated `GET /internal/schedules/maintenance` dispatcher and retained `GET /internal/cron/<name>` adapters require a different `CRON_SECRET`. Use distinct random secrets of 32–512 non-whitespace characters. Viewer credentials authorize neither path; never put job credentials in query strings.

| Action | Durable cadence | Maximum run |
| --- | --- | --- |
| `scheduling` | Each dispatcher window | 150 seconds |
| `drafts-cleanup` | 15 minutes | 45 seconds |
| `discovery` | Each dispatcher window | 30 seconds |
| `ttl-cleanup` | 1 hour | 45 seconds, bounded batches |
| `rss-poll` | 30 minutes | 90 seconds |
| `analytics-cleanup` | 24 hours | 45 seconds |
| `author-history` | Each dispatcher window | 90 seconds |
| `read-state-recovery` | Each dispatcher window | 60 seconds |

`vercel.ts` declares one Production dispatcher every 15 minutes and one hourly Development dispatcher only for `APP_ENV=dev`, Preview target, branch `dev`. Other previews/local declare no schedules. Together these use at most 3,720 scheduled runs in a 31-day month against Hobby's shared 5,000-run allowance. Delivery can arrive up to 15 minutes late, and failures can delay work further. New/edited posts use quarter-hour times and visible approximate-publishing copy; existing off-grid queued posts remain publishable.

`JOBS_RUNNER_MODE=external` disables internal maintenance timers and heartbeat. Serverless mode also starts no history or telemetry export loops; request telemetry flushes within a bounded request scope. Author history and public PDS read-state recovery persist work/checkpoints for later invocations. Duplicate maintenance invocations do not bypass action leases or durable cadence fences.

## Encrypted Private Attachments

Hosted storage selects `SOCIAL_DRAFT_STORAGE_BACKEND=vercel-blob` with server-only `BLOB_READ_WRITE_TOKEN`. Each environment has a private store; preserve the existing `SOCIAL_DRAFT_STORAGE_KEY`, random object keys and server ciphertext. The adapter authenticates private reads, rejects redirects, and bounds response bodies. S3 remains supported for local/recovery and migration source access.

Files retain the 100 MiB limit by uploading sequential 3 MiB parts through `app.thesocialwire.socialDraft.uploadAttachmentChunk`, then `completeAttachmentUpload`. Parts use the reserved UUID, zero-based part header and exact expected length, with at most 34 parts. The server reserves immutable encrypted metadata and a random private object key before storing an encrypted temporary part. Identical retries preserve reservation/ciphertext; changed bytes conflict. Completion authenticates every part and checks the reserved whole-file SHA-256 and content type before storing the established encrypted attachment format.

Temporary objects expire with the 24-hour reservation or retire after completion and are removed by drafts cleanup. Durable owner leases fence mutations/completion/deletion; bounded transfer leases cap temporary spools. Downloads authenticate the complete file before streaming flushed 64 KiB chunks without Content-Length. Local/client copies survive failed or unconfirmed transfers.

For S3-to-Blob migration, `draft-object-migrate --receipt /absolute/private-receipt.json` copies ciphertext without decrypting or re-encrypting it. Freeze writers first; explicitly select the source/destination and verify destination SHA-256/length. Atomic private receipts bind both stores and refuse conflicting existing objects. Database snapshots, matching objects and unchanged encryption keys form one recovery set. Do not retire a bucket based on database migration alone.

## Local Images and Recovery

Portable images remain supported for local verification and recovery:

```sh
docker build -f apps/web/Dockerfile -t social-wire-web .
docker build -f services/appview/Dockerfile.standalone -t social-wire-backend .
(cd services/appview && go test -vet=all -count=1 ./...)
bash scripts/ci/verify-runtime-images.sh appview
```

`cmd/appview` runs the combined local backend. Local SQLite stores migrate before listening. The Docker entrypoint requires one persistent `/data` mount and one writer, rejects symlinked databases and drops privileges; remote Turso mode requires no local volume. Preserve non-root execution and file permissions. Server-only Web credentials never belong in image build arguments.

`sqlite-backup /absolute/new-backup.sqlite` creates a no-overwrite, owner-only snapshot using SQLite's online pager API, including committed WAL pages, then verifies integrity and foreign keys. Set `SQLITE_DB_PATH` to the correct AppView or scheduling source and copy verified snapshots off the source volume. Never copy only a live main database file. Inspect standalone offline copies with `database-preflight --kind appview|scheduling --snapshot /absolute/offline.sqlite` before import; it rejects unsafe sidecars, schema dependencies and unclean scheduling vaults.

Remote backups use Turso's snapshot/log recovery procedure. A main export can omit recent committed log data; retain and replay the matching log before treating it as complete. Rehearse restore and retain unchanged encryption keys and object receipts. Rolling back to an older provider after new private writes requires reverse transfer or reconciliation; changing the image/URL alone hides those writes.

Historical Railway/Swift/Postgres transitions remain available in Git and explicitly historical runbooks. Current operator resource retirement and acceptance evidence belong in issue #963; do not restart the frozen legacy writer as an automatic fallback.
