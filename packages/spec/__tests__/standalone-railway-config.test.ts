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
    // Railway rejects generic cache IDs; one portable image serves both environments.
    expect(dockerfile).not.toContain("--mount=type=cache");
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


describe("standalone Railway Frontend contract", () => {
  it("runs the same final Node image verified by CI", () => {
    const web = JSON.parse(readFileSync(join(root, "railway/web-standalone.json"), "utf8"));
    const image = readFileSync(join(root, "apps/web/Dockerfile"), "utf8");
    expect(web.build.builder).toBe("DOCKERFILE");
    expect(web.build.dockerfilePath).toBe("/apps/web/Dockerfile");
    expect(web.deploy.startCommand).toBeNull();
    expect(web.deploy.preDeployCommand).toBeNull();
    expect(web.deploy.numReplicas).toBe(1);
    expect(web.deploy.multiRegionConfig).toEqual({ sfo: { numReplicas: 1 } });
    expect(web.deploy.healthcheckPath).toBe("/");
    expect(image).toContain('CMD ["node", "apps/web/server.js"]');
    expect(image).toContain("USER node");
    for (const input of ["/package.json", "/bun.lock", "/turbo.json", "/.dockerignore", "/railway/web-standalone.json", "/scripts/ci/**", "/packages/**"]) {
      expect(web.build.watchPatterns).toContain(input);
    }
    expect(web.build.watchPatterns).not.toContain("/scripts/go/**");
  });
});
