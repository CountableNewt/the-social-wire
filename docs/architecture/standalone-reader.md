# Standalone Reader Backend

The target has two Railway services: Next.js Web and one Backend container.
The Backend runs the Go public API, SQLite-backed reader and draft stores,
RSS polling, retention and the embedded scheduler.

PDS records remain authoritative for user-owned protocol data. SQLite owns the
local projections and private unpublished state. The existing encrypted attachment
bucket remains in use. No Postgres, Redis, separate Gateway,
or Coordinator connection is used by standalone mode. The existing distributed
mode remains available during the cutover/rollback window.

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
Keep `/railway/appview.json`, the distributed Dockerfile and `.railway/` graph
as rollback references; do not apply them to the consolidated AppView.

Combine the current Gateway and App View settings.
Preserve the exact existing values of:

- Public OAuth origins, CORS policy, issuer/JWKS policy and
  `PDS_ATTESTATION_RECEIPT_SECRET`.
- L@tr client credentials, GIF key and Pro entitlement settings.
- `SOCIAL_DRAFT_STORAGE_KEY`, draft S3 settings and recovery keys.
- `EMBEDDED_SCHEDULING_STORAGE_KEY` and the scheduling OAuth public origin.
  Keep `/data/scheduling.sqlite` on the same persistent volume.
- `GATEWAY_APPVIEW_INTERNAL_SECRET`: retained as the analytics pseudonym HMAC
  key, despite the removed internal network hop.

Keep global ingestion disabled; standalone mode rejects its activation. The Wire,
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

## Development-First Data Cutover

1. Verify the new images and create a restorable backup of the existing
   environment's Postgres database. Preserve its existing encrypted object
   buckets and keys. Do not delete historical ingestion/checkpoint data.
2. Stop the old environment's writer processes before the final snapshot:
   Gateway/App View and Coordinator. Keep Web in a
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
   read/unread counts, private drafts and attachments, and saved links. Test
   restart persistence. Backend health alone is not acceptance.
7. Accept Development before repeating the same change in Production.
   Preserve the previous images, Postgres database and object stores throughout
   the rollback window. Retire the old compute only after acceptance; database,
   volume and bucket deletion is a separate destructive operator action.

The importer copies retained reader scopes/content, legacy read marks/floors,
PDS authority manifests, private drafts and analytics.
Disposable caches/counters and algorithmic histories remain in the old database.
Already-migrated PDS viewers start with an unready projection and rebuild from
verified authoritative PDS records before serving read state.

## Backups and Rollback

Enable Railway volume backups. For a live consistent SQLite snapshot, run
`sqlite-backup /data/<unique-name>.sqlite`; it uses `VACUUM INTO` to include
committed WAL data, verifies integrity and refuses to overwrite a destination.
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
