import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const backend = JSON.parse(readFileSync(join(root, "services/appview/vercel.json"), "utf8"));
const build = readFileSync(join(root, "services/appview/scripts/vercel-build.sh"), "utf8");

describe("standalone Vercel AppView contract", () => {
  it("routes unchanged API paths to the explicit Go package entrypoint", () => {
    expect(backend.services.appview.root).toBe(".");
    expect(backend.services.appview.framework).toBe("go");
    expect(backend.services.appview.entrypoint).toBe("cmd/appview/main.go");
    expect(backend.services.appview.functions["cmd/appview/main.go"])
      .toEqual({ maxDuration: 300, regions: ["sfo1"] });
    expect(backend.rewrites).toEqual([{ source: "/(.*)", destination: { service: "appview" } }]);
    expect(backend.env).toEqual({ RUNTIME_MODE: "serverless", DATABASE_BACKEND: "turso", JOBS_RUNNER_MODE: "external" });
    expect(backend.builds).toBeUndefined();
    expect(backend.experimentalServices).toBeUndefined();
  });

  it("builds all sibling Go files and gates store initialization to explicitly authorized release builds", () => {
    expect(backend.services.appview.buildCommand).toBe("bash scripts/vercel-build.sh");
    expect(build).toContain("set -euo pipefail");
    expect(build).toContain('-o "$VERCEL_OUTPUT_FILE" ./cmd/appview');
    expect(build).toContain('"$VERCEL_OUTPUT_FILE" --initialize-only');
    expect(build).toContain('${DEPLOY_SCHEMA_INITIALIZATION:-false}');
    expect(build).toContain('${VERCEL_ENV:-}');
    expect(build).toContain('!= "production"');
    expect(build).toContain('dev:dev|prod:main');
    expect(build).toContain('exit 1');
    expect(build.indexOf("go build")).toBeLessThan(build.indexOf('"$VERCEL_OUTPUT_FILE" --initialize-only'));
    // The supported Services schema does not accept this legacy-only property.
    expect(backend.services.appview.preDeployCommand).toBeUndefined();
  });

  it("schedules all eight leased GET adapters without publishing credentials", () => {
    expect(backend.crons).toHaveLength(8);
    expect(new Set(backend.crons.map((cron: { path: string }) => cron.path))).toEqual(new Set([
      "scheduling", "discovery", "drafts-cleanup", "ttl-cleanup", "rss-poll",
      "analytics-cleanup", "author-history", "read-state-recovery",
    ].map(name => `/internal/cron/${name}`)));
    for (const cron of backend.crons) {
      expect(cron.schedule.trim().split(/\s+/)).toHaveLength(5);
    }
    expect(backend.env.CRON_SECRET).toBeUndefined();
    expect(backend.env.JOBS_SECRET).toBeUndefined();
    expect(JSON.stringify(backend)).not.toContain("TURSO_AUTH_TOKEN");
  });
});
