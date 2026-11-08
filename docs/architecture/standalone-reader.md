# Standalone Reader Backend

The target has two Railway services: Next.js Web and one Backend container.
The Backend runs the Swift public API, SQLite-backed reader/draft/podcast stores,
RSS polling and retention. When podcasts are enabled, it also supervises the
existing Bun/FFmpeg processor on `127.0.0.1:9080`. A child exit stops the
container; readiness checks processor startup and health. Media processing never
holds a SQLite transaction during network access or rendering.

PDS records remain authoritative for user-owned protocol data. SQLite owns the
local projections and private unpublished state. Existing encrypted attachment
and podcast media buckets remain in use. No Postgres, Redis, separate Gateway,
or Coordinator connection is used by standalone mode. The existing distributed
mode remains available during the cutover/rollback window.

## Images and Configuration

Build from the repository root:

```sh
docker build -f apps/web/Dockerfile -t social-wire-web .
docker build -f services/appview/Dockerfile.standalone -t social-wire-backend .
docker build --target backend-tests -f services/appview/Dockerfile.standalone .
docker build --target media-tests -f services/appview/Dockerfile.standalone .
```

Web image public build arguments must match the destination environment; see its
Dockerfile and `.tangled/README.md`. Server-only Web credentials stay in Railway,
never in image build arguments.

Reuse the existing Gateway service as Backend to preserve its public domains,
including Development clients using the Railway-generated Gateway hostname.
Add one persistent US-West volume mounted at `/data`, with one replica. Set
`BACKEND_MODE=standalone`, `SQLITE_DB_PATH=/data/social-wire.sqlite`, canonical
`APP_ENV=dev|prod`, and `PORT=8081` (align the existing domain target port).
Use the standalone image's entrypoint; remove the old Postgres schema-ready
start/pre-deploy command and migrator-reference variable on this service.

Combine the current Gateway, App View and enabled Podcast Media Worker settings.
Preserve the exact existing values of:

- Public OAuth origins, CORS policy, issuer/JWKS policy and
  `PDS_ATTESTATION_RECEIPT_SECRET`.
- L@tr client credentials, GIF key and Pro entitlement settings.
- `SOCIAL_DRAFT_STORAGE_KEY`, draft S3 settings and recovery keys.
- `PODCAST_PRIVATE_STORAGE_KEY`, podcast S3 settings,
  `PODCAST_MEDIA_INTERNAL_SECRET` and public asset origin.
- `GATEWAY_APPVIEW_INTERNAL_SECRET`: retained as the analytics pseudonym HMAC
  key, despite the removed internal network hop.

Keep global ingestion and Wire/Circle/Finance/Sports feed modes disabled.
Standalone mode rejects their activation. Preserve the current provider-backed
content discovery and retained feature settings. Stale Redis/Postgres variables
are unused by this mode; remove them after the replacement is accepted.
The supervisor sets the processor URL to loopback; no external media worker
URL or public processor listener is required.

## Development-First Data Cutover

1. Verify the new images and create a restorable backup of the existing
   environment's Postgres database. Preserve its existing encrypted object
   buckets and keys. Do not delete historical ingestion/checkpoint data.
2. Stop the old environment's writer processes before the final snapshot:
   Gateway/App View, Coordinator and Podcast Media Worker. Keep Web in a
   maintenance window. Never let old and new writers run simultaneously.
3. Run a temporary one-shot Railway job using the Backend image and the new
   volume mounted at `/data`. Copy that environment's combined settings and set
   `SOURCE_DATABASE_URL` to its old Postgres private URL. Use start command
   `/usr/local/bin/migrate-from-postgres`, no HTTP health check, and restart
   policy `NEVER`.
4. The job initializes all enabled feature schemas without opening listeners,
   exports a repeatable-read **read-only** Postgres snapshot, imports it in one
   SQLite transaction and verifies row counts, foreign keys and integrity.
   It refuses nonempty retained tables. An error rolls back imported rows.
   Tokens and encrypted payloads are never printed.
5. Verify the job succeeded and retain its row-count receipt. Stop the job,
   attach the same volume to Backend, remove `SOURCE_DATABASE_URL`, and use
   the normal image start command. Set its domain ports and `/readyz` check.
6. Verify the exact deployment is `SUCCESS`, then exercise authenticated
   bootstrap, subscribed/following feeds, PDS read-state migration/recovery,
   read/unread counts, private drafts and attachments, saved links, and enabled
   podcast/private-feed/progress/search/clip flows. Test restart persistence
   and processor failure/restart. Backend health alone is not acceptance.
7. Accept Development before repeating the same change in Production.
   Preserve the previous images, Postgres database and object stores throughout
   the rollback window. Retire the old compute only after acceptance; database,
   volume and bucket deletion is a separate destructive operator action.

The importer copies retained reader scopes/content, legacy read marks/floors,
PDS authority manifests, private drafts, analytics and podcast state/jobs.
Disposable caches/counters and algorithmic histories remain in the old database.
Already-migrated PDS viewers start with an unready projection and rebuild from
verified authoritative PDS records before serving read state. Running podcast
jobs retain their lease expiry and are reclaimed by the new processor after
expiry; the old processor must be stopped first.

## Backups and Rollback

Enable Railway volume backups. For a live consistent SQLite snapshot, run
`sqlite-backup /data/<unique-name>.sqlite`; it uses `VACUUM INTO` to include
committed WAL data, verifies integrity and refuses to overwrite a destination.
Copy verified snapshots off the source volume and retain the matching object
buckets and encryption keys. Never copy only the live main SQLite file.

Restore only while both API and processor are stopped. Verify the snapshot,
restore its owner-only file permissions, and start one Backend replica. A
rollback to the old Postgres system after new private writes requires a reverse
migration or explicit reconciliation; swapping the image alone would hide
those writes. Prefer rolling the standalone image back while preserving its
SQLite volume. Retained Postgres is the pre-cutover recovery point.

Canonical source/CI/image migration follows runtime acceptance; see
[the Tangled delivery configuration](../../.tangled/README.md).
