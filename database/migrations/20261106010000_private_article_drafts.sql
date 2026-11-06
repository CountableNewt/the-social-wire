-- Canonical private article payloads; never replicated to ATProto repositories.
CREATE TABLE IF NOT EXISTS private_article_drafts (
 owner TEXT NOT NULL REFERENCES private_social_draft_accounts(owner),
 draft_id UUID NOT NULL,
 revision BIGINT NOT NULL CHECK (revision > 0),
 deleted BOOLEAN NOT NULL DEFAULT false,
 mutation_id UUID NOT NULL,
 mutation_hash BYTEA NOT NULL,
 payload BYTEA,
 payload_bytes INTEGER NOT NULL CHECK (payload_bytes >= 0 AND payload_bytes <= 524288),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(owner,draft_id),
 CHECK ((deleted AND payload IS NULL AND payload_bytes=0) OR (NOT deleted AND payload IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS private_article_draft_bindings (
 owner TEXT NOT NULL,
 draft_id UUID NOT NULL,
 local_asset_id UUID NOT NULL,
 asset_id UUID NOT NULL,
 PRIMARY KEY(owner,draft_id,local_asset_id),
 FOREIGN KEY(owner,draft_id) REFERENCES private_article_drafts(owner,draft_id),
 FOREIGN KEY(owner,asset_id) REFERENCES private_social_draft_assets(owner,asset_id)
);
CREATE INDEX IF NOT EXISTS private_article_draft_binding_assets ON private_article_draft_bindings(owner,asset_id);
