import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

const repositoryRoot = join(import.meta.dir, "../../..");
const workflow = readFileSync(join(repositoryRoot, ".github/workflows/ci.yml"), "utf8");
const parsed = Bun.YAML.parse(workflow) as any;
const verifier = readFileSync(join(repositoryRoot, "scripts/ci/verify-runtime-images.mjs"), "utf8");
const backendImage = readFileSync(join(repositoryRoot, "services/appview/Dockerfile.standalone"), "utf8");

describe("standalone reader CI", () => {
  it("requires only the two packaged runtime targets and path detection", () => {
    expect(Object.keys(parsed.jobs)).toEqual(expect.arrayContaining(["changes", "web", "appview", "apple", "required", "warm_base_cache"]));
    expect(parsed.jobs.required.name).toBe("CI — Required");
    expect(parsed.jobs.required.needs).toEqual(["changes", "web", "appview", "apple"]);
    expect(parsed.jobs.required.if).toBe("always()");
    expect(parsed.jobs.web.needs).toBe("changes");
    expect(parsed.jobs.web.steps.some((step: any) => step.run === "bash scripts/ci/verify-runtime-images.sh web")).toBe(true);
    expect(parsed.jobs.appview.name).toBe("SQLite AppView");
    expect(parsed.jobs.appview.needs).toEqual(["changes", "appview_go", "appview_image"]);
    expect(parsed.jobs.appview.if).toStartWith("always()");
    for (const name of ["appview_go", "appview_image"]) {
      expect(parsed.jobs[name].needs).toBe("changes");
      expect(parsed.jobs[name]["timeout-minutes"]).toBeLessThanOrEqual(30);
    }
    const verify = parsed.jobs.appview_go.steps.find((step: any) => step.name === "Verify AppView sources");
    expect(verify.run).toContain("go test -vet=all -count=1 ./...");
    expect(parsed.jobs.appview_go.defaults.run["working-directory"]).toBe("services/appview");
    expect(parsed.jobs.appview_image.steps.some((step: any) => step.run?.includes("bash scripts/ci/verify-runtime-images.sh appview"))).toBe(true);
    expect(workflow).not.toContain("working-directory: packages/swift");
    expect(workflow).not.toMatch(/working-directory: services\/(?:gateway|indexing-worker)/);
    expect(parsed.jobs.apple["runs-on"]).toBe("macos-26");
    expect(parsed.jobs.apple.steps.some((step: any) => step.run === "bash scripts/ci/apple-test.sh")).toBe(true);
    expect(workflow).not.toContain("REDIS_INTEGRATION_URL");
    expect(workflow).not.toContain("DATABASE_URL");
  });

  it("restores the Go build cache and warms the base branch from exact PR proof", () => {
    const steps = parsed.jobs.appview_go.steps;
    const setup = steps.find((step: any) => step.uses === "actions/setup-go@v6");
    expect(setup.with["go-version-file"]).toBe("services/appview/go.mod");
    expect(setup.with.cache).toBe(false);
    const restore = steps.find((step: any) => step.uses === "actions/cache/restore@v5");
    const verify = steps.findIndex((step: any) => step.name === "Verify AppView sources");
    const archive = steps.findIndex((step: any) => step.name === "Archive verified Go build cache");
    expect(steps.indexOf(restore)).toBeLessThan(verify);
    expect(archive).toBeGreaterThan(verify);
    expect(restore.with.key).toContain("appview-go-v1-");
    expect(restore.with.key).toContain("hashFiles('services/appview/go.sum')");
    expect(restore.with.key).toEndWith("${{ github.sha }}");
    const warmer = parsed.jobs.warm_appview_cache;
    expect(warmer.if).toContain("needs.changes.outputs.reuse_appview == 'true'");
    expect(warmer["continue-on-error"]).toBe(true);
    expect(parsed.jobs.required.needs).not.toContain("warm_appview_cache");
    const downloads = warmer.steps.filter((step: any) => step.uses === "actions/download-artifact@v8");
    expect(downloads.map((step: any) => step.with.name)).toEqual(["appview-go", "docker-cache-appview"]);
    for (const download of downloads) expect(download.with["run-id"]).toBe("${{ needs.changes.outputs.cache_run_id }}");
    const save = warmer.steps.find((step: any) => step.name === "Save Go build cache in base-branch cache scope");
    expect(save.with.key).toBe(restore.with.key);
    expect(save.with.path).toBe(restore.with.path);
  });

  it("preserves read-only merge verification and Development-first promotion", () => {
    expect(parsed.permissions).toEqual({ contents: "read" });
    expect(parsed.on).toHaveProperty("pull_request");
    expect(parsed.on).toHaveProperty("merge_group");
    expect(parsed.on).toHaveProperty("workflow_dispatch");
    // Required release proof also covers the merged branch SHA.
    expect(parsed.on.push).toEqual({ branches: ["main", "dev"] });
    const steps = parsed.jobs.changes.steps;
    expect(steps.some((step: any) => step.run === "node scripts/ci/validated-merge.mjs --receipt")).toBe(true);
    expect(steps.some((step: any) => step.run === "node --test scripts/ci/validated-merge.test.mjs")).toBe(true);
    const parity = steps.find((step: any) => step.name === "Require Production changes in Development first");
    expect(parity.if).toContain("pull_request.base.ref == 'main'");
    expect(parity.if).toContain("merge_group.base_ref == 'refs/heads/main'");
    expect(parity.if).toContain("github.event_name == 'push' && github.ref == 'refs/heads/main'");
    expect(parity.env.PRODUCTION_BASE_SHA).toBe("${{ github.event.pull_request.base.sha || github.event.merge_group.base_sha || github.event.before }}");
    expect(parity.env.PRODUCTION_HEAD_SHA).toBe("${{ github.event.pull_request.head.sha || github.event.merge_group.head_sha || github.sha }}");
    expect(parity.run).toContain("git show refs/remotes/origin/dev:scripts/ci/development-parity.mjs");
    expect(parity.run).toContain('--development refs/remotes/origin/dev');
    const detector = steps.find((step: any) => step.id === "filter");
    expect(detector.env.GITHUB_EVENT_BEFORE).toBe("${{ github.event.before }}");
    expect(detector.env.GITHUB_EVENT_PULL_REQUEST_BASE_SHA).toBe("${{ github.event.pull_request.base.sha }}");
  });

  it("checks real release images and SQLite migrations", () => {
    expect(verifier).not.toContain("backend-tests");
    expect(verifier).toContain("await runtimeVolumeGuardSmoke(image)");
    expect(verifier).toContain("await runtimeSmoke(image)");
    expect(verifier).toContain("await webRuntimeSmoke(buildImage(kind))");
    expect(parsed.jobs.appview_go.steps.find((step: any) => step.name === "Verify AppView sources").run)
      .toContain("go test -vet=all -count=1 ./...");
    expect(backendImage).toContain("FROM golang:");
    expect(backendImage).toContain("CGO_ENABLED=0");
    expect(backendImage).toContain("-o /out/AppView ./cmd/appview");
    expect(backendImage).toContain("COPY --from=builder /out/AppView /usr/local/bin/AppView");
    expect(backendImage.indexOf("go mod download")).toBeLessThan(backendImage.indexOf("COPY services/appview/internal ./internal"));
    expect(backendImage).not.toMatch(/swift/i);
    expect(backendImage).not.toMatch(/podcast|ffmpeg|oven\/bun|bun install/i);
    expect(backendImage).toContain("BACKEND_MODE=standalone SQLITE_DB_PATH=/data/social-wire.sqlite");
    expect(verifier).toContain("status('http://127.0.0.1:8081/readyz') === '200'");
    expect(verifier).toContain("expectStatus('http://127.0.0.1:8081/livez', '200')");
  });

  it("retains Web coverage, type/lint, lexicon/schema and shared read-state checks", () => {
    for (const command of ["bunx turbo typecheck --filter=web", "bunx turbo lint --filter=web", "bun --cwd packages/read-state typecheck", "bun --cwd packages/read-state test", "bun run test:coverage", "bun --cwd packages/lexicons test", "cd packages/spec && bun test --timeout 30000"]) {
      expect(verifier).toContain(command);
    }
    expect(verifier).toContain('coverage_status=$?');
    expect(verifier).toContain('exit "$coverage_status"');
    expect(verifier).not.toContain("check-out-swift-contracts");
  });

  function runRequiredGate(overrides: Record<string, string | undefined> = {}) {
    const gate = parsed.jobs.required.steps.find((step: any) => step.name === "Verify path-filtered checks");
    const baseline = {
      CHANGES_RESULT: "success", WEB_FLAG: "false", WEB_RESULT: "skipped", APPVIEW_FLAG: "false", APPVIEW_RESULT: "skipped",
      APPLE_FLAG: "false", APPLE_RESULT: "skipped", REUSE_WEB: "false", REUSE_APPVIEW: "false", GITHUB_EVENT_NAME: "pull_request", GITHUB_REF: "refs/pull/123/merge",
    };
    return spawnSync("bash", ["-c", gate.run], { cwd: repositoryRoot, env: { ...process.env, ...baseline, ...overrides }, encoding: "utf8" });
  }

  it("fails the aggregate gate for every selected check that does not succeed", () => {
    for (const flag of ["WEB", "APPVIEW", "APPLE"]) {
      for (const result of ["failure", "cancelled", "skipped", "success"]) {
        expect(runRequiredGate({ [`${flag}_FLAG`]: "true", [`${flag}_RESULT`]: result }).status).toBe(result === "success" ? 0 : 1);
      }
    }
    for (const result of ["failure", "cancelled", "skipped"]) {
      expect(runRequiredGate({ CHANGES_RESULT: result }).status).toBe(1);
    }
    expect(runRequiredGate().status).toBe(0);
  });

  it("rejects missing or malformed native path-selection outputs", () => {
    for (const value of [undefined, "", "TRUE"]) {
      expect(runRequiredGate({ APPLE_FLAG: value }).status).toBe(1);
    }
  });

  it("accepts proven target skips only on dev and main pushes", () => {
    for (const target of ["WEB", "APPVIEW"]) {
      const proof = { [`${target}_FLAG`]: "true", [`${target}_RESULT`]: "skipped", [`REUSE_${target}`]: "true" };
      for (const branch of ["dev", "main"]) {
        expect(runRequiredGate({ ...proof, GITHUB_EVENT_NAME: "push", GITHUB_REF: `refs/heads/${branch}` }).status).toBe(0);
        for (const result of ["failure", "cancelled", "success"]) {
          expect(runRequiredGate({ ...proof, GITHUB_EVENT_NAME: "push", GITHUB_REF: `refs/heads/${branch}`, [`${target}_RESULT`]: result }).status).toBe(1);
        }
      }
      for (const event of ["pull_request", "merge_group", "workflow_dispatch"]) {
        expect(runRequiredGate({ ...proof, GITHUB_EVENT_NAME: event, GITHUB_REF: "refs/heads/dev" }).status).toBe(1);
      }
      expect(runRequiredGate({ ...proof, GITHUB_EVENT_NAME: "push", GITHUB_REF: "refs/heads/feature/unproven" }).status).toBe(1);
    }
  });

  it("fails closed when path or proof outputs are missing or malformed", () => {
    for (const target of ["WEB", "APPVIEW"]) {
      for (const output of [`${target}_FLAG`, `REUSE_${target}`]) {
        for (const value of [undefined, "", "TRUE"]) expect(runRequiredGate({ [output]: value }).status).toBe(1);
      }
      expect(runRequiredGate({ [`REUSE_${target}`]: "true", GITHUB_EVENT_NAME: "push", GITHUB_REF: "refs/heads/dev" }).status).toBe(1);
    }
  });

});
