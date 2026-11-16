import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const migration = readFileSync(
  resolve(
    import.meta.dir,
    "../../../database/migrations/20260730010000_subscribed_feed_membership_index.sql"
  ),
  "utf8"
);
const keysetIndexesMigration = readFileSync(
  resolve(
    import.meta.dir,
    "../../../database/migrations/20260523180000_add_appview_projection_caches.sql"
  ),
  "utf8"
);

describe("Subscribed feed performance migration", () => {
  test("backfills the canonical subscribed membership marker", () => {
    expect(migration).toContain(`section_keys || '["subscribed"]'::jsonb`);
    expect(migration).toContain(`section_keys ? 'my'`);
    expect(migration).toContain(`section_keys ? 'subscribed:unfoldered'`);
    expect(migration).toContain(`section_key.value LIKE 'folder:%'`);
    expect(migration).toContain(`WHERE NOT section_keys ? 'subscribed'`);
  });

  test("adds a JSONB membership index", () => {
    expect(migration).toContain(
      "CREATE INDEX IF NOT EXISTS idx_appview_publication_scopes_section_keys"
    );
    expect(migration).toContain(
      "ON appview_publication_scopes USING GIN (section_keys)"
    );
  });

  test("adds author, site, and stable keyset indexes", () => {
    expect(keysetIndexesMigration).toContain(
      "idx_content_items_author_created_uri"
    );
    expect(keysetIndexesMigration).toContain(
      "ON content_items (author_did, created_at DESC, uri DESC)"
    );
    expect(keysetIndexesMigration).toContain(
      "idx_content_items_author_site_created_uri"
    );
    expect(keysetIndexesMigration).toContain(
      "ON content_items (author_did, publication_site, created_at DESC, uri DESC)"
    );
  });
});
