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

describe("CI path detection", () => {
  it("checks podcast and infrastructure contracts when the Development graph changes", () => {
    const result = detect(repositoryWithChange(".railway/podcasts.ts"), "pull_request");
    expect(result.get("podcast_worker")).toBe("true");
    expect(result.get("spec")).toBe("true");
  });
  it.each([
    "services/gateway/Sources/Gateway/GatewayCommand.swift",
    "services/appview/Package.swift",
    "services/indexing-worker/Tests/WorkerTests/RuntimeTests.swift",
    "tools/schema-ready/Sources/SchemaReady/SchemaReadyMain.swift",
    "services/gateway/Dockerfile",
    "railway/appview.json",
  ])("requires the Swift backend matrix for active runtime changes: %s", (path) => {
    expect(detect(repositoryWithChange(path), "pull_request").get("swift_backend")).toBe("true");
  });

  it.each([
    "services/operations/Sources/Operations/Command.swift",
    "services/jetstream-ingest/Sources/IngressCore/ArchiveTransport.swift",
    "services/wire-corpus-edge/Package.swift",
    "services/wire-worker/README.md",
  ])("does not add inactive services to the Swift migration: %s", (path) => {
    expect(detect(repositoryWithChange(path), "pull_request").get("swift_backend")).toBe("false");
  });

  it("checks the podcast worker when the local Go module changes", () => {
    const result = detect(repositoryWithChange("packages/go/go.mod"), "pull_request");
    expect(result.get("podcast_worker")).toBe("true");
  });

  it("tests portable read-state changes in native and server consumers", () => {
    const result = detect(repositoryWithChange("packages/swift/ReadStateCore/Sources/ReadStateCore/State.swift"), "pull_request");
    for (const job of ["apple", "shared_swift"]) {
      expect(result.get(job)).toBe("true");
    }
  });

  it("tests shared TypeScript read-state changes in the Web job", () => {
    const result = detect(repositoryWithChange("packages/read-state/src/index.ts"), "pull_request");
    expect(result.get("web")).toBe("true");
  });
  it("uses the push before SHA instead of running the full matrix", () => {
    const result = detect(repositoryWithChange("docs/wiki/Testing.md"), "push");
    expect(result.get("docs")).toBe("true");
    expect(result.get("web")).toBe("false");
  });

  it("maps Apple changes to Apple and cross-client contract checks", () => {
    const result = detect(
      repositoryWithChange("apps/apple/SocialWire/Feature.swift"),
      "pull_request",
    );
    expect(result.get("apple")).toBe("true");
    expect(result.get("spec")).toBe("true");
    expect(result.get("web")).toBe("false");
  });

  it("checks every main-schema consumer when its migration manifest changes", () => {
    const result = detect(
      repositoryWithChange("database/migrations/20990101000000_example.sql"),
      "pull_request",
    );
    expect(result.get("shared_swift")).toBe("true");
    expect(result.get("wire_corpus_edge")).toBe("true");
    expect(result.get("database_migrator")).toBe("true");
    expect(result.get("podcast_worker")).toBe("true");
    expect(result.get("spec")).toBe("true");
  });

  it("checks all consumers embedding the shared startup gate", () => {
    const result = detect(
      repositoryWithChange("services/jetstream-ingest/internal/schemaready/gate.go"),
      "pull_request",
    );
    expect(result.get("podcast_worker")).toBe("true");
    expect(result.get("wire_corpus_edge")).toBe("false");
  });

  it("checks retained Redis and Swift contracts when their shared package changes", () => {
    const result = detect(
      repositoryWithChange("packages/swift/SocialWireRedis/Sources/SocialWireRedis/RedisCacheClient.swift"),
      "pull_request",
    );
    expect(result.get("redis")).toBe("true");
    expect(result.get("shared_swift")).toBe("true");
  });

  it("runs deterministic spec coverage when the migration runner changes", () => {
    const result = detect(
      repositoryWithChange("scripts/apply-database-migrations.sh"),
      "pull_request",
    );
    expect(result.get("database_migrator")).toBe("true");
    expect(result.get("spec")).toBe("true");
  });

  it("runs both Bun coverage jobs when their shared inventory gate changes", () => {
    const result = detect(
      repositoryWithChange("scripts/check-bun-coverage-inventory.ts"),
      "pull_request",
    );
    expect(result.get("web")).toBe("true");
    expect(result.get("operations_web")).toBe("true");
  });

  it("runs scope-policy drift checks for Jetstream admission changes", () => {
    const result = detect(
      repositoryWithChange("services/jetstream-ingest/internal/store/postgres.go"),
      "pull_request",
    );
    expect(result.get("spec")).toBe("true");
  });

  it("runs the retired-generation policy spec when its runbook changes", () => {
    const result = detect(
      repositoryWithChange(
        "docs/runbooks/operations/jetstream-v2-durable-replay.md",
      ),
      "pull_request",
    );
    expect(result.get("spec")).toBe("true");
    expect(result.get("operations_web")).toBe("true");
  });

  it("runs Python benchmark safety tests when a tool or its tests change", () => {
    for (const path of [
      "scripts/benchmarks/railway_memory_adapters.py",
      "scripts/benchmarks/tests/test_memory_step_acceptance.py",
    ]) {
      const result = detect(repositoryWithChange(path), "pull_request");
      expect(result.get("benchmark_tools")).toBe("true");
      expect(result.get("spec")).toBe("true");
    }
    const unrelated = detect(repositoryWithChange("docs/wiki/Testing.md"), "pull_request");
    expect(unrelated.get("benchmark_tools")).toBe("false");
  });

  for (const path of ["scripts/ci-prepare-postgres.sh",
    ".github/workflows/validated-merge.yml", ".github/actions/prepare-postgres/action.yml"]) {
    it(`runs the full matrix when shared PostgreSQL preparation changes: ${path}`, () => {
      const output = detect(repositoryWithChange(path), "pull_request");
      expect(output.size).toBe(13);
      expect([...output.values()].every(value => value === "true")).toBe(true);
    });
  }

  it.each(["scripts/ci/validated-merge.mjs", "scripts/ci/validated-merge.test.mjs"])("checks only verifier contracts for helper-only changes: %s", path => {
    const output = detect(repositoryWithChange(path), "pull_request");
    expect(output.get("spec")).toBe("true");
    for (const [job, enabled] of output) {
      if (job !== "spec") expect(enabled, job).toBe("false");
    }
  });

  it("runs the full matrix when the detector changes", () => {
    const result = detect(
      repositoryWithChange("scripts/ci-detect-changes.sh"),
      "pull_request",
    );
    expect([...result.values()].every((value) => value === "true")).toBe(true);
  });

  it("runs the full matrix for an initial push without a base commit", () => {
    const result = detect(
      repositoryWithChange("README.md"),
      "push",
      "0000000000000000000000000000000000000000",
    );
    expect([...result.values()].every((value) => value === "true")).toBe(true);
  });
});

describe("extracted Swift contract path detection", () => {
  it.each([
    "packages/go/migration/extracted-swift-packages.json",
    "scripts/go/check-out-swift-contracts.py",
  ])("checks every extracted-contract consumer when %s changes", (path) => {
    const result = detect(repositoryWithChange(path), "pull_request");
    for (const job of ["redis", "shared_swift", "spec"]) {
      expect(result.get(job), job).toBe("true");
    }
  });
});
