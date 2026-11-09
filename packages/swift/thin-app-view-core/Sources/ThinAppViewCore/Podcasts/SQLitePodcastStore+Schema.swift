import GRDB

extension SQLitePodcastStore {
  static func migrate(_ database: Database) throws {
    try database.execute(
      sql: """

        CREATE TABLE IF NOT EXISTS podcast_shows (
         id text PRIMARY KEY, feed_url text UNIQUE, source_kind text NOT NULL CHECK(source_kind IN ('rss','atproto')),
         source_uri text UNIQUE, show_json TEXT NOT NULL, updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL))
        );
        CREATE TABLE IF NOT EXISTS podcast_episodes (
         id text PRIMARY KEY, show_id text NOT NULL REFERENCES podcast_shows(id), guid text,
         episode_json TEXT NOT NULL, published_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)), updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL))
        );
        CREATE INDEX IF NOT EXISTS podcast_episodes_show_page ON podcast_episodes(show_id,published_at DESC,id DESC);
        CREATE UNIQUE INDEX IF NOT EXISTS podcast_episodes_guid ON podcast_episodes(show_id,guid) WHERE guid IS NOT NULL;
        CREATE TABLE IF NOT EXISTS podcast_aliases (
         alias text PRIMARY KEY, canonical_id text NOT NULL, entity_kind text NOT NULL CHECK(entity_kind IN ('show','episode'))
        );
        CREATE TABLE IF NOT EXISTS podcast_subscriptions (
         viewer_did text NOT NULL, show_id text NOT NULL REFERENCES podcast_shows(id), source_uri text NOT NULL,
         updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)), PRIMARY KEY(viewer_did,show_id)
        );
        CREATE INDEX IF NOT EXISTS podcast_subscriptions_show ON podcast_subscriptions(show_id);
        CREATE TABLE IF NOT EXISTS podcast_viewer_state (
         viewer_did text PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
         state_json TEXT NOT NULL, updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL))
        );
        CREATE TABLE IF NOT EXISTS podcast_jobs (
         id TEXT PRIMARY KEY, viewer_did text, episode_id text REFERENCES podcast_episodes(id),
         kind text NOT NULL CHECK(kind IN ('bridge','silence','clip','transcript','cleanup')),
         status text NOT NULL DEFAULT 'queued' CHECK(status IN ('queued','running','complete','failed')),
         dedupe_key text UNIQUE NOT NULL, payload_json TEXT NOT NULL, result_json TEXT,
         attempts integer NOT NULL DEFAULT 0, available_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)),
         lease_until REAL, worker_id text, error text, created_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)), updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL))
        );
        CREATE INDEX IF NOT EXISTS podcast_jobs_claim ON podcast_jobs(status,available_at,lease_until);
        CREATE TABLE IF NOT EXISTS podcast_clips (
         id TEXT PRIMARY KEY, viewer_did text NOT NULL, episode_id text NOT NULL REFERENCES podcast_episodes(id),
         clip_json TEXT NOT NULL, published_uri text UNIQUE, created_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)), updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL))
        );
        CREATE INDEX IF NOT EXISTS podcast_clips_viewer ON podcast_clips(viewer_did,updated_at DESC);


        CREATE TABLE IF NOT EXISTS podcast_private_shows (
         viewer_did text NOT NULL, id text NOT NULL, feed_hash text NOT NULL,
         feed_data text NOT NULL, show_data text NOT NULL, updated_at REAL NOT NULL DEFAULT (CAST(strftime('%s','now') AS REAL)),
         PRIMARY KEY(viewer_did,id), UNIQUE(viewer_did,feed_hash)
        );
        CREATE TABLE IF NOT EXISTS podcast_private_episodes (
         viewer_did text NOT NULL, id text NOT NULL, show_id text NOT NULL,
         episode_data text NOT NULL, published_at REAL NOT NULL,
         PRIMARY KEY(viewer_did,id),
         FOREIGN KEY(viewer_did,show_id) REFERENCES podcast_private_shows(viewer_did,id) ON DELETE CASCADE
        );
        CREATE INDEX IF NOT EXISTS podcast_private_episodes_page
         ON podcast_private_episodes(viewer_did,show_id,published_at DESC,id DESC);

        """)
  }
}
