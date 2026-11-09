import GRDB

extension SQLiteSocialDraftService {
  static func migrate(_ db: Database) throws {
    try db.execute(sql: """
      CREATE TABLE IF NOT EXISTS private_social_draft_accounts(owner TEXT PRIMARY KEY);
      CREATE TABLE IF NOT EXISTS private_social_draft_assets(
        owner TEXT NOT NULL REFERENCES private_social_draft_accounts(owner), asset_id TEXT NOT NULL,
        metadata BLOB NOT NULL, byte_length INTEGER NOT NULL CHECK(byte_length>0 AND byte_length<=104857600),
        object_key TEXT NOT NULL UNIQUE, state TEXT NOT NULL CHECK(state IN ('reserved','ready','deleted','purged')),
        created_at REAL NOT NULL, PRIMARY KEY(owner,asset_id));
      CREATE TABLE IF NOT EXISTS private_social_draft_manifests(
        owner TEXT NOT NULL REFERENCES private_social_draft_accounts(owner), draft_id TEXT NOT NULL,
        revision INTEGER NOT NULL CHECK(revision>0), deleted INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(owner,draft_id));
      CREATE TABLE IF NOT EXISTS private_social_draft_bindings(
        owner TEXT NOT NULL,draft_id TEXT NOT NULL,position INTEGER NOT NULL,asset_id TEXT NOT NULL,
        role TEXT NOT NULL,ref TEXT NOT NULL,PRIMARY KEY(owner,draft_id,position),
        FOREIGN KEY(owner,draft_id) REFERENCES private_social_draft_manifests(owner,draft_id),
        FOREIGN KEY(owner,asset_id) REFERENCES private_social_draft_assets(owner,asset_id));
      CREATE INDEX IF NOT EXISTS private_social_draft_binding_assets ON private_social_draft_bindings(owner,asset_id);
      CREATE TABLE IF NOT EXISTS private_social_draft_cleanup(
        owner TEXT NOT NULL,asset_id TEXT NOT NULL,available_at REAL NOT NULL,attempts INTEGER NOT NULL DEFAULT 0,
        PRIMARY KEY(owner,asset_id),FOREIGN KEY(owner,asset_id) REFERENCES private_social_draft_assets(owner,asset_id));
      CREATE INDEX IF NOT EXISTS private_social_draft_cleanup_due ON private_social_draft_cleanup(available_at,owner,asset_id);
      CREATE TABLE IF NOT EXISTS private_article_drafts(
        owner TEXT NOT NULL REFERENCES private_social_draft_accounts(owner),draft_id TEXT NOT NULL,
        revision INTEGER NOT NULL CHECK(revision>0),deleted INTEGER NOT NULL DEFAULT 0,mutation_id TEXT NOT NULL,
        mutation_hash BLOB NOT NULL,payload BLOB,payload_bytes INTEGER NOT NULL CHECK(payload_bytes>=0 AND payload_bytes<=524288),
        updated_at REAL NOT NULL,PRIMARY KEY(owner,draft_id),
        CHECK((deleted=1 AND payload IS NULL AND payload_bytes=0) OR (deleted=0 AND payload IS NOT NULL)));
      CREATE TABLE IF NOT EXISTS private_article_draft_bindings(
        owner TEXT NOT NULL,draft_id TEXT NOT NULL,local_asset_id TEXT NOT NULL,asset_id TEXT NOT NULL,
        PRIMARY KEY(owner,draft_id,local_asset_id),
        FOREIGN KEY(owner,draft_id) REFERENCES private_article_drafts(owner,draft_id),
        FOREIGN KEY(owner,asset_id) REFERENCES private_social_draft_assets(owner,asset_id));
      CREATE INDEX IF NOT EXISTS private_article_draft_binding_assets ON private_article_draft_bindings(owner,asset_id);
      """)
  }
}
