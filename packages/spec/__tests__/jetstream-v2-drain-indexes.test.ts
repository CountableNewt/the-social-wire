import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const repositoryRoot = join(import.meta.dir, "../../..");
const migration = readFileSync(
  join(
    repositoryRoot,
    "database/migrations/20260816210000_jetstream_v2_drain_query_indexes.sql",
  ),
  "utf8",
);
const scopeFilterMigration = readFileSync(
  join(
    repositoryRoot,
    "database/migrations/20260816220000_jetstream_v2_scope_filtered_terminal.sql",
  ),
  "utf8",
);
const liveVerification = readFileSync(
  join(repositoryRoot, "scripts/verify-jetstream-v2-drain-indexes.sql"),
  "utf8",
);
const workflow = readFileSync(
  join(repositoryRoot, ".github/workflows/ci.yml"),
  "utf8",
);

describe("Jetstream V2 rolling-drain query indexes", () => {
  it("uses provider-neutral PostgreSQL without hosted-provider roles", () => {
    expect(migration).not.toContain("service_role");
    expect(migration).not.toContain("authenticated");
    expect(migration).not.toContain("CREATE EXTENSION");
    expect(migration).toContain("-- socialwire:transaction=off");
    expect(migration).toContain("CREATE INDEX CONCURRENTLY IF NOT EXISTS");
    expect(migration).toContain("DROP INDEX CONCURRENTLY IF EXISTS");
    expect(migration).toContain("NOT index_state.indisvalid");
  });

  it("splits ready work from expired leases", () => {
    expect(migration).toContain(
      "idx_appview_ingestion_inbox_ready",
    );
    expect(migration).toContain(
      "(environment, source_generation, next_attempt_at, seq)",
    );
    expect(migration).toContain("WHERE status IN ('pending', 'retry')");
    expect(migration).toContain(
      "idx_appview_ingestion_inbox_expired_lease",
    );
    expect(migration).toContain(
      "(environment, source_generation, lease_expires_at, seq)",
    );
    expect(migration).toContain("WHERE status = 'leased'");

  });

  it("indexes generation-scoped reconciliation blocking and FIFO", () => {
    expect(migration).toContain(
      "idx_appview_ingestion_reconciliation_ready",
    );
    expect(migration).toContain(
      "(environment, source_generation, next_attempt_at, trigger_seq, id)",
    );
    expect(migration).toContain(
      "idx_appview_ingestion_reconciliation_expired_lease",
    );
    expect(migration).toContain(
      "(environment, source_generation, lease_expires_at, trigger_seq, id)",
    );
    expect(migration).toContain(
      "idx_appview_ingestion_reconciliation_active_repo",
    );
    expect(migration).toContain(
      "(environment, source_generation, repo_did, trigger_seq)",
    );
    expect(migration).toContain("WHERE status IN ('pending', 'leased')");

  });

  it("indexes the first nonterminal watermark barrier", () => {
    expect(scopeFilterMigration).toContain(
      "idx_appview_ingestion_inbox_terminal_barrier_v2",
    );
    expect(scopeFilterMigration).toContain(
      "ON appview_ingestion_inbox (environment, source_generation, seq)",
    );
    expect(scopeFilterMigration).toContain(
      "WHERE status NOT IN ('applied', 'filtered_scope') AND reconciled_at IS NULL",
    );
  });

  it("adds the publication-first cache invalidation path", () => {
    expect(migration).toContain(
      "idx_first_page_cache_publication_viewer",
    );
    expect(migration).toContain(
      "ON first_page_cache (publication_id, viewer_did)",
    );
  });

  it("preserves historical PostgreSQL plan verification outside the SQLite runtime gate", () => {
    for (const indexName of [
      "idx_appview_ingestion_inbox_ready",
      "idx_appview_ingestion_inbox_expired_lease",
      "idx_appview_ingestion_reconciliation_ready",
      "idx_appview_ingestion_reconciliation_expired_lease",
      "idx_appview_ingestion_reconciliation_active_repo",
      "idx_appview_ingestion_inbox_terminal_barrier",
      "idx_appview_ingestion_inbox_terminal_barrier_v2",
      "idx_first_page_cache_publication_viewer",
    ]) {
      expect(liveVerification).toContain(indexName);
    }
    expect(liveVerification).toContain("EXPLAIN (COSTS OFF)");
    expect(liveVerification).toContain("assert_plan_uses_all");
    expect(liveVerification).toContain("FOR UPDATE SKIP LOCKED");
    expect(liveVerification).toContain(
      "Exercise the corresponding representative reconciliation claim shape",
    );
    const active = Bun.YAML.parse(workflow) as any;
    expect(active.jobs["database-migrator"]).toBeUndefined();
    expect(active.jobs.appview_lanes.steps.some((step: any) => step.name === "Verify AppView lane" && step.run.includes("scripts/ci/benchmark-appview-swiftpm.sh"))).toBe(true);
  });
});
