import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const read = (path: string) => readFileSync(join(root, path), "utf8");

describe("Archived ingress Swift image dependencies", () => {
  it("builds both native products with a frozen dependency graph and native zstd", () => {
    const dockerfile = read("services/jetstream-ingest/Dockerfile");
    expect(dockerfile).toContain("FROM swift:6.4.2-jammy AS builder");
    expect(dockerfile).toContain("libzstd-dev");
    expect(dockerfile).toContain("libzstd1");
    expect(dockerfile).toContain("Package.resolved");
    expect(dockerfile).toContain("--force-resolved-versions");
    for (const executable of ["jetstream-ingest", "schema-ready"]) {
      expect(dockerfile).toContain(`--product ${executable}`);
      expect(dockerfile).toContain(`/out/bin/${executable}`);
    }
    expect(dockerfile).toContain("--generate-manifest /src/database/migrations");
    expect(dockerfile).toContain('ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/jetstream-ingest"]');
    expect(dockerfile).not.toContain("COPY packages/go");
    expect(dockerfile).not.toContain(".swiftpm");
  });

  it("keeps the archived standalone ingress build reproducible", () => {
    const config = JSON.parse(read("railway/jetstream-ingest.json"));
    expect(config.build.dockerfilePath).toBe("/services/jetstream-ingest/Dockerfile");
    expect(config.build.watchPatterns).toContain("/services/jetstream-ingest/**");
    expect(config.build.watchPatterns).toContain("/database/migrations/**");
    expect(config.build.watchPatterns).not.toContain("/packages/go/**");
  });

  it("prevents automatic global ingestion restarts in consolidated IaC", () => {
    const iac = read(".railway/railway.ts");
    const ingress = iac.slice(iac.indexOf('const ingressController = service('), iac.indexOf('const projectionPool = service('));
    expect(ingress).toContain('"/__global_ingress_retired__/**"');
    expect(ingress).not.toContain('"/services/jetstream-ingest/**"');
    expect(ingress).not.toContain('"/packages/go/**"');
    // The dormant standalone Wire ingress is outside this migration.
    const dormant = JSON.parse(read("railway/wire-jetstream-ingest.json"));
    expect(dormant.build.watchPatterns).toContain("/packages/go/**");
  });
});
