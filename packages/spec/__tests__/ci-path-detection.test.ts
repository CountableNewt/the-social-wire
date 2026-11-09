import { afterEach, describe, expect, it } from "bun:test";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { execFileSync } from "node:child_process";

const repositoryRoot = join(import.meta.dir, "../../..");
const detector = join(repositoryRoot, "scripts/ci-detect-changes.sh");
const temporaryRepositories: string[] = [];

afterEach(() => {
  for (const path of temporaryRepositories.splice(0)) {
    rmSync(path, { recursive: true, force: true });
  }
});

function git(cwd: string, ...args: string[]): string {
  return execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
}

function repositoryWithChange(path: string): {
  cwd: string;
  base: string;
  head: string;
} {
  const cwd = mkdtempSync(join(tmpdir(), "socialwire-ci-paths-"));
  temporaryRepositories.push(cwd);
  git(cwd, "init", "--initial-branch=main");
  git(cwd, "config", "user.name", "CI Test");
  git(cwd, "config", "user.email", "ci@example.invalid");
  git(cwd, "config", "commit.gpgsign", "false");
  writeFileSync(join(cwd, "README.md"), "base\n");
  mkdirSync(join(cwd, dirname(path)), { recursive: true });
  writeFileSync(join(cwd, path), "before\n");
  git(cwd, "add", ".");
  git(cwd, "commit", "-m", "base");
  const base = git(cwd, "rev-parse", "HEAD");
  writeFileSync(join(cwd, path), "after\n");
  git(cwd, "add", ".");
  git(cwd, "commit", "-m", "change");
  return { cwd, base, head: git(cwd, "rev-parse", "HEAD") };
}

function detect(
  repository: ReturnType<typeof repositoryWithChange>,
  eventName: "push" | "pull_request",
  before = repository.base,
): Map<string, string> {
  const output = join(repository.cwd, "github-output.txt");
  execFileSync("bash", [detector], {
    cwd: repository.cwd,
    env: {
      ...process.env,
      GITHUB_BASE_REF: "",
      GITHUB_EVENT_BEFORE: eventName === "push" ? before : "",
      GITHUB_EVENT_NAME: eventName,
      GITHUB_EVENT_PULL_REQUEST_BASE_SHA:
        eventName === "pull_request" ? repository.base : "",
      GITHUB_OUTPUT: output,
      GITHUB_SHA: repository.head,
    },
  });
  return new Map(
    readFileSync(output, "utf8")
      .trim()
      .split("\n")
      .map((line) => line.split("=", 2) as [string, string]),
  );
}

describe("standalone CI path detection", () => {
  it.each([
    "services/appview/Package.swift",
    "services/appview/Dockerfile.standalone",
    "services/appview/bin/backend-supervisor",
    "services/podcast-worker/src/index.ts",
    "packages/swift/thin-app-view-core/Sources/ThinAppViewCore/SQLiteThinAppViewStore.swift",
    "scripts/migrations/postgres_to_sqlite.py",
    "database/migrations/20990101000000_example.sql",
    "railway/appview-standalone.json",
    "package.json", "bun.lock", ".dockerignore",
  ])("checks backend runtime plus schema/source contracts for %s", path => {
    const result = detect(repositoryWithChange(path), "pull_request");
    expect(Object.fromEntries(result)).toEqual({ web: "true", appview: "true" });
  });
  it.each([
    "apps/web/src/app/page.tsx",
    "packages/read-state/src/index.ts",
    "packages/swift/ReadStateCore/Sources/ReadStateCore/State.swift",
    "packages/lexicons/lexicons/article.json",
    "packages/spec/__tests__/example.test.ts",
    "apps/apple/SocialWire/Feature.swift",
    "scripts/go/check-out-swift-contracts.py",
    "packages/go/migration/extracted-swift-packages.json",
    "services/gateway/Sources/Gateway/GatewayCommand.swift",
    "services/indexing-worker/Tests/WorkerTests/RuntimeTests.swift",
    "railway/web.json",
  ])("retains cross-client/source contracts without distributed runtime jobs for %s", path => {
    expect(Object.fromEntries(detect(repositoryWithChange(path), "pull_request"))).toEqual({ web: "true", appview: "false" });
  });
  it.each(["scripts/ci/verify-runtime-images.mjs", "scripts/ci/verify-runtime-images.test.mjs", "scripts/ci/development-parity.mjs"])("checks both packaged verifiers when %s changes", path => {
    expect([...detect(repositoryWithChange(path), "pull_request").values()]).toEqual(["true", "true"]);
  });
  it.each([".github/workflows/ci.yml", ".github/workflows/validated-merge.yml", "scripts/ci-detect-changes.sh"])("requires every runtime check when shared gate %s changes", path => {
    expect([...detect(repositoryWithChange(path), "pull_request").values()]).toEqual(["true", "true"]);
  });
  it("uses push before SHA and skips unrelated documentation-only changes", () => {
    expect(Object.fromEntries(detect(repositoryWithChange("docs/wiki/Testing.md"), "push"))).toEqual({ web: "false", appview: "false" });
  });
  it.each([
    ["services/appview/Sources/AppView/AppViewCommand.swift", { web: "true", appview: "true" }],
    ["apps/web/src/app/page.tsx", { web: "true", appview: "false" }],
    [".github/workflows/ci.yml", { web: "true", appview: "true" }],
  ])("selects runtime checks from the pushed commit range for %s", (path, expected) => {
    expect(Object.fromEntries(detect(repositoryWithChange(path as string), "push"))).toEqual(expected);
  });
  it("runs both targets for an initial push without a base commit", () => {
    expect([...detect(repositoryWithChange("README.md"), "push", "0".repeat(40)).values()]).toEqual(["true", "true"]);
  });
});
