import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const migration = readFileSync(
  join(root, "database/migrations/20260908173000_index_wire_account_and_metadata_work.sql"),
  "utf8",
);

describe("Wire account and metadata work indexes", () => {
  it("can safely resume an interrupted concurrent build", () => {
    expect(migration).toStartWith("-- socialwire:transaction=off");
    for (const index of ["wire_items_author_idx", "wire_link_metadata_general_due_idx"]) {
      expect(migration).toContain(`DROP INDEX CONCURRENTLY IF EXISTS public.${index}`);
      expect(migration).toContain(`CREATE INDEX CONCURRENTLY IF NOT EXISTS ${index}`);
      expect(migration).toContain(`to_regclass('public.${index}')`);
    }
    expect(migration).toContain("NOT indisvalid");
    expect(migration).toContain("AND indisvalid AND indisready");
    expect(migration).toContain("RESET statement_timeout");
    expect(migration).toContain("RESET lock_timeout");
  });

  it("orders general claims null-first without changing due rules", () => {
    expect(migration).toMatch(/WHERE status IN \([^)]+\)/);
    expect(migration).toContain(
      "(language_checked_at ASC NULLS FIRST, retry_after, canonical_key)",
    );
    // Time-dependent eligibility remains in the claim, never frozen in an index predicate.
    expect(migration).not.toMatch(/WHERE[^;]*(retry_after|fresh_until|NOW\()/);
    expect(migration).toContain("ON public.wire_items (author_key)");
  });
});
