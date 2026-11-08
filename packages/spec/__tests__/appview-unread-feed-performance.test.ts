import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dir, "../../..");
const migration = readFileSync(
  resolve(
    root,
    "database/migrations/20260804190000_appview_publication_scope_keys.sql"
  ),
  "utf8"
);

describe("AppView unread feed performance", () => {
  test("normalizes rebuildable publication scope keys with cascading ownership", () => {
    expect(migration).toContain(
      "CREATE TABLE IF NOT EXISTS appview_publication_scope_keys"
    );
    expect(migration).toContain(
      "PRIMARY KEY (viewer_did, publication_id, scope_key)"
    );
    expect(migration).toContain("ON DELETE CASCADE");
    expect(migration).toContain("jsonb_array_elements_text(scope.scope_keys)");
    expect(migration).toContain(
      "idx_appview_publication_scope_keys_content"
    );
    expect(migration).toContain(
      "CREATE TRIGGER appview_publication_scopes_refresh_keys"
    );
    expect(migration).toContain(
      "AFTER INSERT OR UPDATE OF author_did, scope_keys"
    );
  });
});
