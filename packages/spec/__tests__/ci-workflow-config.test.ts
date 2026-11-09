import { describe, expect, it } from "bun:test";
import { existsSync, readFileSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";

const repositoryRoot = join(import.meta.dir, "../../..");
const workflow = readFileSync(join(repositoryRoot, ".github/workflows/ci.yml"), "utf8");
const parsed = Bun.YAML.parse(workflow) as any;
const verifier = readFileSync(join(repositoryRoot, "scripts/ci/verify-runtime-images.mjs"), "utf8");
const backendImage = readFileSync(join(repositoryRoot, "services/appview/Dockerfile.standalone"), "utf8");

describe("standalone reader CI", () => {
  it("requires only the two packaged runtime targets and path detection", () => {
    expect(Object.keys(parsed.jobs)).toEqual(["changes", "web", "appview", "required"]);
    expect(parsed.jobs.required.name).toBe("CI — Required");
    expect(parsed.jobs.required.needs).toEqual(["changes", "web", "appview"]);
    expect(parsed.jobs.required.if).toBe("always()");
    for (const [job, selection] of [["web", "web"], ["appview", "appview"]]) {
      const value = parsed.jobs[job];
      expect(value.needs).toBe("changes");
      expect(value.if).toContain(`outputs.${selection} == 'true'`);
      expect(value["timeout-minutes"]).toBeGreaterThanOrEqual(60);
      expect(value.steps.some((step: any) => step.run === `bash scripts/ci/verify-runtime-images.sh ${selection}`)).toBe(true);
      expect(value.services).toBeUndefined();
    }
    expect(workflow).not.toMatch(/runs-on: macos-|working-directory: services\/(?:gateway|indexing-worker)/);
    expect(workflow).not.toContain("REDIS_INTEGRATION_URL");
    expect(workflow).not.toContain("DATABASE_URL");
  });

  it("restores per-runtime build caches before verification and saves fresh commit layers", () => {
    for (const kind of ["web", "appview"]) {
      const steps = parsed.jobs[kind].steps;
      const setup = steps.findIndex((step: any) => step.uses === "docker/setup-buildx-action@v3");
      const cache = steps.findIndex((step: any) => step.uses === "actions/cache@v5");
      const verify = steps.findIndex((step: any) => step.run === `bash scripts/ci/verify-runtime-images.sh ${kind}`);
      expect(setup).toBeGreaterThan(-1);
      expect(cache).toBeGreaterThan(setup);
      expect(verify).toBeGreaterThan(cache);
      expect(steps[cache].with.key).toContain(`docker-${kind}-v1-`);
      expect(steps[cache].with.key).toEndWith("${{ github.sha }}");
      expect(steps[cache].with["restore-keys"]).toContain(`docker-${kind}-v1-`);
      expect(steps[cache].with.path).toBe(steps[verify].env.CI_DOCKER_CACHE_DIR);
    }
  });

  it("preserves read-only merge verification and Development-first promotion", () => {
    expect(parsed.permissions).toEqual({ contents: "read" });
    expect(parsed.on).toHaveProperty("pull_request");
    expect(parsed.on).toHaveProperty("merge_group");
    expect(parsed.on).toHaveProperty("workflow_dispatch");
    // Railway's wait-for-checks gate needs a suite on the merged branch SHA.
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

  it("checks real release images, SQLite migrations, and the bundled processor", () => {
    expect(verifier).toContain("buildImage(kind, 'backend-tests')");
    expect(verifier).toContain("buildImage(kind, 'media-tests')");
    expect(verifier).toContain("await runtimeVolumeGuardSmoke(image)");
    expect(verifier).toContain("await runtimeSmoke(image)");
    expect(verifier).toContain("await webRuntimeSmoke(buildImage(kind))");
    expect(backendImage).toContain("FROM builder AS backend-tests");
    expect(backendImage).toContain("python3 -m unittest discover -s scripts/migrations -p 'test_*.py'");
    expect(backendImage).toContain("swift test --package-path services/appview -c release --force-resolved-versions");
    expect(backendImage).toContain("swift test --package-path packages/swift/thin-app-view-core");
    expect(backendImage).toContain("bun --cwd services/podcast-worker typecheck && bun --cwd services/podcast-worker test");
    expect(backendImage).toContain("BACKEND_MODE=standalone SQLITE_DB_PATH=/data/social-wire.sqlite");
    expect(verifier).toContain("expectStatus('http://127.0.0.1:8081/readyz', '503')");
    expect(verifier).toContain("expectStatus('http://127.0.0.1:8081/livez', '200')");
  });

  it("retains Web coverage, type/lint, lexicon/schema and shared read-state checks", () => {
    for (const command of ["bunx turbo typecheck --filter=web", "bunx turbo lint --filter=web", "bun --cwd packages/read-state typecheck", "bun --cwd packages/read-state test", "bun run test:coverage", "bun --cwd packages/lexicons test", "cd packages/spec && bun test --timeout 30000"]) {
      expect(verifier).toContain(command);
    }
    expect(verifier).toContain('coverage_status=$?');
    expect(verifier).toContain('exit "$coverage_status"');
    expect(verifier.indexOf("run('python3', ['scripts/go/check-out-swift-contracts.py'])")).toBeLessThan(verifier.indexOf("const image = buildImage(kind, 'builder')"));
  });

  it("fails the aggregate gate for every selected check that does not succeed", () => {
    const gate = parsed.jobs.required.steps.find((step: any) => step.run);
    const baseline = { CHANGES_RESULT: "success", WEB_FLAG: "false", WEB_RESULT: "skipped", APPVIEW_FLAG: "false", APPVIEW_RESULT: "skipped" };
    for (const flag of ["WEB", "APPVIEW"]) {
      for (const result of ["failure", "cancelled", "skipped", "success"]) {
        const run = spawnSync("bash", ["-c", gate.run], { env: { ...process.env, ...baseline, [`${flag}_FLAG`]: "true", [`${flag}_RESULT`]: result }, encoding: "utf8" });
        expect(run.status).toBe(result === "success" ? 0 : 1);
      }
    }
    for (const result of ["failure", "cancelled", "skipped"]) {
      expect(spawnSync("bash", ["-c", gate.run], { env: { ...process.env, ...baseline, CHANGES_RESULT: result } }).status).toBe(1);
    }
    expect(spawnSync("bash", ["-c", gate.run], { env: { ...process.env, ...baseline } }).status).toBe(0);
  });
});

describe("extracted Swift reference fixtures", () => {
  it("pins offline checkouts and rejects changed, mismatched or inherited repositories", () => {
    const root = mkdtempSync(join(tmpdir(), "swift-contract-checkout-"));
    const git = (cwd: string, ...args: string[]) => {
      const result = spawnSync("git", ["-C", cwd, ...args], { encoding: "utf8" });
      if (result.status !== 0) throw new Error(result.stderr);
      return result.stdout.trim();
    };
    const source = join(root, "source");
    const destination = join(root, "packages/swift/FixtureCore");
    const manifest = join(root, "manifest.json");
    const run = (...args: string[]) => spawnSync("python3", [
      join(repositoryRoot, "scripts/go/check-out-swift-contracts.py"),
      "--root", root, "--manifest", manifest, ...args,
    ], { encoding: "utf8" });
    try {
      mkdirSync(source);
      git(source, "init", "--quiet");
      git(source, "config", "user.name", "Contract Test");
      git(source, "config", "user.email", "test@example.invalid");
      git(source, "config", "commit.gpgsign", "false");
      writeFileSync(join(source, "Package.swift"), "original\n");
      git(source, "add", ".");
      git(source, "commit", "--quiet", "-m", "original");
      const revision = git(source, "rev-parse", "HEAD");
      const pin = (value = revision) => writeFileSync(manifest, JSON.stringify({
        schemaVersion: 1, packages: [{ name: "FixtureCore", repository: source, revision: value }],
      }));
      pin();
      expect(run("--package", "UnknownCore").status).not.toBe(0);
      expect(existsSync(destination)).toBe(false);
      expect(run().status).toBe(0);
      expect(git(destination, "rev-parse", "HEAD")).toBe(revision);
      expect(run().status).toBe(0);
      writeFileSync(join(destination, "Package.swift"), "local work\n");
      expect(run().stderr).toContain("local changes");
      expect(readFileSync(join(destination, "Package.swift"), "utf8")).toBe("local work\n");
      git(destination, "checkout", "--", "Package.swift");
      git(destination, "remote", "set-url", "origin", join(root, "wrong"));
      expect(run().stderr).toContain("unexpected origin");
      git(destination, "remote", "set-url", "origin", source);
      pin("0".repeat(40));
      expect(run().stderr).toContain("pinned revision");
      pin("main");
      expect(run().stderr).toContain("full immutable Git revision");
      pin();
      rmSync(destination, { recursive: true });
      mkdirSync(destination);
      writeFileSync(join(destination, "keep.txt"), "keep\n");
      expect(run().stderr).toContain("independent Git checkout");
      expect(readFileSync(join(destination, "keep.txt"), "utf8")).toBe("keep\n");
      rmSync(destination, { recursive: true });
      pin("0".repeat(40));
      expect(run().status).not.toBe(0);
      expect(existsSync(destination)).toBe(false);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
