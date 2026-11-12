# Embedded scheduling cutover

The Social Wire Web backend prepares Standard.site records locally. Immediate article publishing continues to write through the viewer's PDS session. Scheduled posts and articles keep their `at.skej.schedule` records, schedule URIs, and publication keys. Standalone AppView owns the queue and runs the scheduler after cutover.

## Development cutover

1. Release the code to Development. On the Development backend configure `EMBEDDED_SCHEDULING_PUBLIC_ORIGIN=https://api.testing.thesocialwire.app`, a fresh 32-byte Base64 `EMBEDDED_SCHEDULING_STORAGE_KEY`, and `EMBEDDED_SCHEDULING_ENABLED=true`. Keep `EMBEDDED_SCHEDULING_IMPORT_LEGACY=false` while the standalone worker is active. AppView processes jobs created through the embedded scheduler during this phase.
2. Verify `/oauth/scheduling/client-metadata.json`, then set `NEXT_PUBLIC_USE_EMBEDDED_SCHEDULING=true` and `NEXT_PUBLIC_ENABLE_POST_SCHEDULING=true` on Development Web and rebuild. Connect from Content Calendar with the test account and verify background publication of a scheduled post and article. Verify the published URIs use the intended publication keys.
3. Before importing existing jobs, stop the standalone Skej scheduler for the same environment and confirm it has stopped. Preserve its volume and record the state of jobs that were in flight. Pause writes through legacy clients so no new jobs enter the old queue.
4. Set `EMBEDDED_SCHEDULING_IMPORT_LEGACY=true` on the backend. Connect scheduling for each personal or linked brand account. For accounts already connected to the embedded scheduler, reopening Content Calendar triggers recovery. AppView preserves schedule identities and holds jobs with recorded prior attempts for review. Queued jobs already past their schedule can run after import.
5. Keep the storage key with protected recovery material. The separate `scheduling.sqlite` database lives alongside the AppView database on its persistent volume. Run one scheduler deployment against that volume. Include the database in backups by setting `SQLITE_DB_PATH=/data/scheduling.sqlite` when invoking `sqlite-backup` with a separate destination.

Skej OAuth grants are tied to the old client ID. They cannot be moved to the new client by copying the SQLite database. Keep the old volume for queue reconciliation and recovery. For environments with server-side Pro gating, the selected scheduling DID must be entitled, including a linked brand DID.

AnyPub's private drafts, local assets and publish history live on its own volume. Export or preserve that volume and account for pending publications before deleting it. Published PDS records remain available independently of that volume.

If the new scheduler has accepted connections or schedules, pause scheduling and reconcile the PDS records against both queue snapshots before re-enabling any old worker. Keep a single publisher active throughout. Repeat the cutover for Production only after Development acceptance and before retiring the Production services.
