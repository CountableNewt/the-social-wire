import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const config = JSON.parse(readFileSync(join(root, "railway/appview-standalone.json"), "utf8"));
const dockerfile = readFileSync(join(root, "services/appview/Dockerfile.standalone"), "utf8");

describe("standalone Railway AppView contract", () => {
  it("keeps one durable SQLite writer and uses the packaged supervisor", () => {
    expect(config.build.builder).toBe("DOCKERFILE");
    expect(config.build.dockerfilePath).toBe("/services/appview/Dockerfile.standalone");
    expect(config.deploy.requiredMountPath).toBe("/data");
    expect(config.deploy.numReplicas).toBe(1);
    expect(config.deploy.region).toBe("sfo");
    expect(config.deploy.multiRegionConfig).toEqual({ sfo: { numReplicas: 1 } });
    expect(config.deploy.sleepApplication).toBe(false);
    expect(config.deploy.overlapSeconds).toBe(0);
    expect(config.deploy.startCommand).toBeNull();
    expect(config.deploy.preDeployCommand).toBeNull();
    expect(config.deploy.healthcheckPath).toBe("/readyz");
    expect(config.deploy.restartPolicyType).toBe("ALWAYS");
    expect(dockerfile).toContain('ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/standalone-entrypoint"]');
    expect(dockerfile).toContain('CMD ["/usr/local/bin/AppView"]');
    expect(dockerfile).toContain("util-linux");
  });

  it("redeploys when any standalone Docker COPY input changes", () => {
    const patterns = config.build.watchPatterns as string[];
    const watched = (path: string) => patterns.some(pattern => pattern.endsWith("/**")
      ? path.startsWith(pattern.slice(0, -2)) : path === pattern);
    for (const line of dockerfile.split("\n")) {
      if (!line.startsWith("COPY ") || line.startsWith("COPY --from=")) continue;
      const sources = line.slice("COPY ".length).trim().split(/\s+/).slice(0, -1);
      for (const source of sources) {
        expect(watched(`/${source}`) || watched(`/${source}/changed-input`), source).toBe(true);
      }
    }
    for (const path of ["/bun.lock", "/.dockerignore", "/railway/appview-standalone.json"]) {
      expect(watched(path), path).toBe(true);
    }
    expect(watched("/docs/unrelated.md")).toBe(false);
    expect(patterns).not.toContain("/database/migrations/**");
    expect(patterns).not.toContain("/tools/schema-ready/**");
  });
});
