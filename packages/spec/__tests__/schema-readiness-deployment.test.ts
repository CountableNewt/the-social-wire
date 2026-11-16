import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const read = (path: string) => readFileSync(join(root, path), "utf8");
const images = {
  "indexing-worker": "IndexingWorker",
  appview: "AppView",
  gateway: "Gateway",
};

describe("schema readiness deployment contract", () => {
  for (const [service, executable] of Object.entries(images)) {
    it(`${service} gates its existing executable with an image-owned manifest`, () => {
      const dockerfile = read(`services/${service}/Dockerfile`);
      const entrypoint = JSON.parse(dockerfile.match(/^ENTRYPOINT (.+)$/m)![1]);
      expect(entrypoint[0]).toBe("/usr/local/bin/schema-ready");
      expect(entrypoint.slice(-2)).toEqual(["--", `/usr/local/bin/${executable}`]);
      // Every service image requires PostgreSQL; local SQLite remains a
      // package development backend, never a service-image startup bypass.
      expect(entrypoint.includes("--allow-local-sqlite")).toBe(false);
      expect(dockerfile).toContain("COPY database/migrations /src/database/migrations");
      expect(dockerfile).toContain("--generate-manifest /src/database/migrations");
      expect(dockerfile).toContain("/etc/socialwire/required-migrations.txt");
    });
  }

  for (const service of ["gateway", "appview"]) {
    it(`${service} rebuilds on schema/gate changes without bypassing the entrypoint`, () => {
      const config = JSON.parse(read(`railway/${service}.json`));
      expect(config.deploy.startCommand).toBeUndefined();
      expect(config.build.watchPatterns).toContain("/database/migrations/**");
      expect(config.build.watchPatterns).toContain("/tools/schema-ready/**");
      expect(config.deploy.healthcheckTimeout).toBeGreaterThan(90);
    });
  }

  it("keeps maintenance on the standalone schema-gated image", () => {
    const iac = read(".railway/railway.ts");
    const build = iac.slice(iac.indexOf("const indexingBuild"), iac.indexOf("const longRunningDeploy"));
    const deploy = iac.slice(iac.indexOf("const longRunningDeploy"), iac.indexOf("export default"));
    expect(build).toContain('dockerfilePath: "/services/indexing-worker/Dockerfile"');
    for (const path of ["/services/indexing-worker/**", "/database/migrations/**",
      "/tools/schema-ready/**"]) {
      expect(build).toContain(`"${path}"`);
    }
    expect(iac.match(/build: indexingBuild/g)).toHaveLength(1);
    expect(iac).toContain('watchPatterns: ["/__global_projection_retired__/**"]');
    expect(iac).toContain('THIN_APPVIEW_WORKER_MODE: "maintenance"');
    expect(iac).toContain('OPERATIONS_RECOVERY_ENABLED: "false"');
    expect(iac.match(/deploy: longRunningDeploy/g)).toHaveLength(1);
    expect(deploy).toContain('healthcheckPath: "/startupz"');
    expect(Number(deploy.match(/healthcheckTimeout: (\d+)/)?.[1])).toBeGreaterThan(90);
    const dockerfile = read("services/indexing-worker/Dockerfile");
    expect(dockerfile).toContain("FROM swift:6.4.2-jammy");
    expect(dockerfile).toContain("--product IndexingWorker");
    expect(dockerfile).toContain("--product retry-reconciliation");
    expect(dockerfile).not.toContain("COPY packages/go");
  });

  it("keeps retired intake and projection dormant while RSS maintenance can restart", () => {
    const iac = read(".railway/railway.ts");
    const retiredDeploy = iac.slice(iac.indexOf("const retiredWorkerDeploy"), iac.indexOf("export default"));
    const projection = iac.slice(iac.indexOf('const projectionPool = service('), iac.indexOf('const coordinator = service('));
    const coordinator = iac.slice(iac.indexOf('const coordinator = service('), iac.indexOf('return project('));
    expect(retiredDeploy).toContain('restartPolicyType: "NEVER"');
    expect(projection).toContain("deploy: retiredWorkerDeploy");
    expect(projection).not.toContain("deploy: longRunningDeploy");
    expect(coordinator).toContain("deploy: longRunningDeploy");
    expect(coordinator).toContain("build: indexingBuild");
    expect(coordinator).toContain('THIN_APPVIEW_WORKER_MODE: "maintenance"');
    expect(coordinator).toContain('THIN_APPVIEW_RSS_FEED_POLL_ENABLED: "true"');
    expect(iac).toContain("projectionPool, coordinator,");
    expect(iac).not.toContain("Ingress Controller");
  });

  it("keeps the consolidated IaC entrypoint and rebuild dependencies", () => {
    const iac = read(".railway/railway.ts");
    expect(iac).not.toContain("startCommand:");
    expect(iac).toContain('"/tools/schema-ready/**"');
    expect(iac).toContain('"/database/migrations/**"');
  });

  it("does not gate the database migrator", () => {
    expect(read("services/database-migrator/Dockerfile")).not.toContain("schema-ready");
  });

  it("gates the podcast worker with the Swift schema-ready image stage", () => {
    const dockerfile = read("services/podcast-worker/Dockerfile");
    expect(dockerfile).toContain("--product schema-ready --static-swift-stdlib");
    expect(dockerfile).toContain("--generate-manifest /src/database/migrations");
    expect(dockerfile).not.toMatch(/golang|go build/);
    const entrypoint = JSON.parse(dockerfile.match(/^ENTRYPOINT (.+)$/m)![1]);
    expect(entrypoint.slice(0, 2)).toEqual(["/usr/local/bin/schema-ready", "--"]);
  });
});
