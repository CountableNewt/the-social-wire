import { describe, expect, it } from "bun:test";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { appViewSurfaces, appViewSurfaceForPath, socialWireVercelConfig, type AppViewSurface } from "../../../scripts/vercel-config";
import { registeredGoRoutes } from "./helpers/goRoutes";

const root = join(import.meta.dir, "../../..");
const backend = socialWireVercelConfig("prod", "production", "main");

const build = readFileSync(join(root, "services/appview/scripts/vercel-build.sh"), "utf8");

function runBuildScript(surface: string, environment: Record<string, string>, verify: (result: ReturnType<typeof spawnSync>, directory: string) => void) {
  const directory = mkdtempSync(join(tmpdir(), "appview-build-guard-"));
  try {
    writeFileSync(join(directory, "go"), `#!/usr/bin/env bash
set -euo pipefail
printf '%s\\n' "$@" > "$TSW_BUILD_ARGUMENTS"
cat > "$VERCEL_OUTPUT_FILE" <<'BINARY'
#!/usr/bin/env bash
printf '%s\\n' "$@" > "$TSW_INITIALIZE_ARGUMENTS"
BINARY
chmod +x "$VERCEL_OUTPUT_FILE"
`, { mode: 0o700 });
    const result = spawnSync("bash", [join(root, "services/appview/scripts/vercel-build.sh"), surface], {
      encoding: "utf8",
      env: {
        PATH: `${directory}:${process.env.PATH}`,
        VERCEL_OUTPUT_FILE: join(directory, "output"),
        TSW_BUILD_ARGUMENTS: join(directory, "build-arguments"),
        TSW_INITIALIZE_ARGUMENTS: join(directory, "initialize-arguments"),
        ...environment,
      },
    });
    verify(result, directory);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

describe("standalone Vercel AppView contract", () => {
  it("renders the root CLI config without changing service paths or enabling generic preview maintenance", () => {
    const result = spawnSync(process.execPath, [join(root, "scripts/render-vercel-config.ts")], {
      encoding: "utf8",
      env: { ...process.env, APP_ENV: "dev", VERCEL_ENV: "preview", VERCEL_GIT_COMMIT_REF: "feature/isolated" },
    });
    expect(result.status, result.stderr).toBe(0);
    expect(JSON.parse(result.stdout)).toEqual(socialWireVercelConfig("dev", "preview", "feature/isolated"));
    expect(JSON.parse(result.stdout).schedules).toEqual({});
  });

  it("only automatically deploys main and dev for the consolidated project, including denying slash-named feature branches", () => {
    // Minimatch's * excludes path separators; ** closes the slash-branch gap.
    // Vercel gives an explicit true match precedence over matching false rules.
    const allowlist = { main: true, dev: true, "*": false, "**": false };
    for (const config of [backend, socialWireVercelConfig("dev", "preview", "dev")]) {
      expect(config.git.deploymentEnabled).toEqual(allowlist);
    }
  });

  it("builds six Go surfaces with unchanged API ingress and runtime bounds", () => {
    expect(appViewSurfaces).toEqual(["reader", "publishing", "scheduling", "analytics", "integrations", "maintenance"]);
    expect(Object.keys(backend.services)).toEqual(["web", ...appViewSurfaces]);
    for (const surface of appViewSurfaces) {
      const service = backend.services[surface]!;
      expect(service.root).toBe("services/appview");
      expect(service.framework).toBe("go");
      expect(service.entrypoint).toBe(`cmd/${surface}/main.go`);
      expect(service.buildCommand).toBe(`bash scripts/vercel-build.sh ${surface}`);
      expect(service.functions[`cmd/${surface}/main.go`])
        .toEqual({ maxDuration: 300, regions: ["sfo1"] });
    }
    expect(backend.rewrites.at(-1)).toEqual({ source: "/(.*)", destination: { service: "web" } });
    expect(backend.env).toEqual({ RUNTIME_MODE: "serverless", DATABASE_BACKEND: "turso", JOBS_RUNNER_MODE: "external" });
    expect(backend).not.toHaveProperty("builds");
    expect(backend).not.toHaveProperty("experimentalServices");
  });

  it("routes each registered Go endpoint to its package owner without changing its path", () => {
    const sourceOwners: { matches: (source: string) => boolean; surface: AppViewSurface }[] = [
      { matches: path => /\/internal\/(drafts|articles)\//.test(path) || path.endsWith("/edge/gif_routes.go"), surface: "publishing" },
      { matches: path => path.includes("/internal/scheduling/backend/"), surface: "scheduling" },
      { matches: path => path.includes("/internal/analytics/"), surface: "analytics" },
      { matches: path => path.includes("/internal/semble/") || path.endsWith("/edge/latr.go"), surface: "integrations" },
      { matches: path => path.includes("/internal/jobs/"), surface: "maintenance" },
    ];
    const routes = registeredGoRoutes();
    const owned = new Set<string>();
    for (const owner of sourceOwners) {
      const registered = registeredGoRoutes(owner.matches);
      expect(registered.size, owner.surface).toBeGreaterThan(0);
      for (const route of registered) {
        const path = route.slice(route.indexOf(" ") + 1);
        expect(appViewSurfaceForPath(path), route).toBe(owner.surface);
        expect(owned.has(route), `duplicate owner: ${route}`).toBe(false);
        owned.add(route);
        // Service destinations retain the original HTTP path, with no path rewrite.
        const rewrite = backend.rewrites.find(item => item.source === path ||
          (item.source.endsWith("(.*)") && path.startsWith(item.source.slice(0, -4))));
        expect(rewrite?.destination).toEqual({ service: owner.surface });
      }
    }
    for (const route of routes) {
      if (owned.has(route)) continue;
      const path = route.slice(route.indexOf(" ") + 1);
      expect(appViewSurfaceForPath(path), route).toBe("reader");
    }
    expect(appViewSurfaceForPath("/v1/publications/sidebar")).toBe("reader");
    expect(appViewSurfaceForPath("/v1/appview/bootstrap-stream")).toBe("reader");
    expect(appViewSurfaceForPath("/oauth-client-metadata.json")).toBe("reader");
  });

  it("routes shared-origin APIs and API hosts while retaining Next pages and handlers", () => {
    const route = (path: string, host = "preview.vercel.app") => backend.rewrites.find(rule => {
      const hostRule = "has" in rule ? rule.has : undefined;
      return (!hostRule || hostRule.some(condition => condition.value === host)) &&
        (rule.source === path || (rule.source.endsWith("(.*)") && path.startsWith(rule.source.slice(0, -4))));
    })?.destination.service;
    for (const registered of registeredGoRoutes()) {
      const path = registered.slice(registered.indexOf(" ") + 1);
      const hostOwnedMetadata = ["/oauth-client-metadata.json", "/oauth/client-metadata.json", "/ios-client-metadata.json"].includes(path);
      expect(route(path), registered).toBe(hostOwnedMetadata ? "web" : appViewSurfaceForPath(path));
      for (const host of ["api.thesocialwire.app", "api.testing.thesocialwire.app"]) {
        expect(route(path, host), `${host}${path}`).toBe(appViewSurfaceForPath(path));
      }
    }
    for (const path of ["/", "/read", "/saved", "/callback", "/client-metadata.json", "/oauth-client-metadata.json", "/ios-client-metadata.json", "/api/oembed", "/api/latr-gateway/v1/latr/saves", "/_next/static/app.js"]) {
      expect(route(path), path).toBe("web");
    }
    for (const host of ["api.thesocialwire.app", "api.testing.thesocialwire.app"]) {
      expect(route("/unknown-native-path", host)).toBe("reader");
    }
    expect(backend.services.web).toEqual({
      root: "apps/web", framework: "nextjs",
      installCommand: "cd ../.. && bun install --frozen-lockfile", buildCommand: "bun run build",
    });
    expect(backend.regions).toEqual(["sfo1"]);
    expect(existsSync(join(root, "apps/web/vercel.json"))).toBe(false);
    expect(existsSync(join(root, "services/appview/vercel.ts"))).toBe(false);
  });

  it("builds all sibling Go files and gates store initialization to explicitly authorized release builds", () => {
    expect(build).toContain("set -euo pipefail");
    expect(build).toContain('-o "$VERCEL_OUTPUT_FILE" "./cmd/$surface"');
    expect(build).toContain('"$VERCEL_OUTPUT_FILE" --initialize-only');
    expect(build).toContain('"$surface" != "maintenance"');
    expect(build).toContain('${DEPLOY_SCHEMA_INITIALIZATION:-false}');
    expect(build).toContain('${VERCEL_ENV:-}');
    expect(build).toContain('!= "production"');
    expect(build).toContain('dev:dev|prod:main');
    expect(build).toContain('exit 1');
    expect(build.indexOf("go build")).toBeLessThan(build.indexOf('"$VERCEL_OUTPUT_FILE" --initialize-only'));
    // The supported Services schema does not accept this legacy-only property.
    for (const surface of appViewSurfaces) {
      expect(backend.services[surface]).not.toHaveProperty("preDeployCommand");
    }
  });

  it("consolidates maintenance into one authenticated path schedule within the shared Hobby quota", () => {
    expect(backend).not.toHaveProperty("crons");
    expect(backend.schedules).toEqual({
      maintenance: {
        schedule: "*/15 * * * *",
        path: "/internal/schedules/maintenance",
        requestMethod: "GET",
        timezone: "UTC",
        jitter: 15,
      },
    });
    const development = socialWireVercelConfig("dev", "preview", "dev");
    expect(development.schedules.maintenance?.schedule).toBe("0 * * * *");
    // One run per quarter-hour in Production and per hour in Development.
    const largestMonthRuns = (4 + 1) * 24 * 31;
    expect(largestMonthRuns).toBe(3720);
    expect(largestMonthRuns).toBeLessThan(5000);
    expect(backend.env).not.toHaveProperty("CRON_SECRET");
    expect(backend.env).not.toHaveProperty("JOBS_SECRET");
    expect(JSON.stringify(backend)).not.toContain("TURSO_AUTH_TOKEN");
  });

  it("schedules only the exact Development Preview branch and leaves other previews or local runs idle", () => {
    expect(socialWireVercelConfig("dev", "preview", "dev").schedules).toEqual({
      maintenance: {
        schedule: "0 * * * *",
        path: "/internal/schedules/maintenance",
        requestMethod: "GET",
        timezone: "UTC",
        jitter: 15,
      },
    });
    for (const appEnvironment of ["prod", "local", "development", "typo", undefined]) {
      expect(socialWireVercelConfig(appEnvironment, "preview", "dev").schedules).toEqual({});
    }
    for (const branch of ["main", "feature/dev", "refs/heads/dev", "Dev", "dev ", "", undefined]) {
      expect(socialWireVercelConfig("dev", "preview", branch).schedules).toEqual({});
    }
    for (const environment of ["development", "local", undefined]) {
      expect(socialWireVercelConfig("prod", environment, "main").schedules).toEqual({});
      expect(socialWireVercelConfig("dev", environment, "dev").schedules).toEqual({});
    }
    expect(socialWireVercelConfig("prod", "preview", "main").schedules).toEqual({});
    expect(socialWireVercelConfig("prod", "production", "main").schedules).toEqual(backend.schedules);
    for (const environment of [undefined, "local", "production", "typo"]) {
      expect(socialWireVercelConfig(environment, "production", "main").schedules).toEqual({});
    }
    for (const branch of [undefined, "dev", "feature/test"]) {
      expect(socialWireVercelConfig("prod", "production", branch).schedules).toEqual({});
    }
  });

  it("builds the Development Preview without automatic shared-store initialization", () => {
    runBuildScript("maintenance", {
      VERCEL_ENV: "preview", APP_ENV: "dev", VERCEL_GIT_COMMIT_REF: "dev",
      DEPLOY_SCHEMA_INITIALIZATION: "false",
    }, (result, directory) => {
      expect(result.status, result.stderr?.toString()).toBe(0);
      expect(existsSync(join(directory, "build-arguments"))).toBe(true);
      expect(existsSync(join(directory, "initialize-arguments"))).toBe(false);
    });
  });

  it("never passes an arbitrary package path to the Go builder", () => {
    runBuildScript("../other-package", {}, (result, directory) => {
      expect(result.status).toBe(1);
      expect(existsSync(join(directory, "build-arguments"))).toBe(false);
    });
  });

  it("initializes stores once from the maintenance build, never from other surface builds", () => {
    const release = { DEPLOY_SCHEMA_INITIALIZATION: "true", VERCEL_ENV: "production", APP_ENV: "dev", VERCEL_GIT_COMMIT_REF: "dev" };
    for (const surface of appViewSurfaces) {
      runBuildScript(surface, release, (result, directory) => {
        expect(result.status, result.stderr?.toString()).toBe(0);
        expect(readFileSync(join(directory, "build-arguments"), "utf8")).toContain(`./cmd/${surface}\n`);
        expect(existsSync(join(directory, "initialize-arguments"))).toBe(surface === "maintenance");
        if (surface === "maintenance") {
          expect(readFileSync(join(directory, "initialize-arguments"), "utf8")).toBe("--initialize-only\n");
        }
      });
    }
  });

  it("rejects requested initialization outside its approved release environment and branch", () => {
    for (const environment of [
      { VERCEL_ENV: "preview", APP_ENV: "dev", VERCEL_GIT_COMMIT_REF: "dev" },
      { VERCEL_ENV: "production", APP_ENV: "prod", VERCEL_GIT_COMMIT_REF: "dev" },
      { VERCEL_ENV: "production", APP_ENV: "dev", VERCEL_GIT_COMMIT_REF: "main" },
    ]) {
      runBuildScript("maintenance", { DEPLOY_SCHEMA_INITIALIZATION: "true", ...environment }, (result, directory) => {
        expect(result.status).toBe(1);
        expect(existsSync(join(directory, "initialize-arguments"))).toBe(false);
      });
    }
  });
});
