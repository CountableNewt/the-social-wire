import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const source = readFileSync(join(root, ".github/workflows/ci.yml"), "utf8");
const ci = Bun.YAML.parse(source) as any;

describe("Bounded CI dependency installation", () => {
  it("uses frozen installs inside verified images instead of unbounded runner installs", () => {
    expect(Object.values(ci.jobs).some((job: any) => job.steps?.some((step: any) => step.run?.match(/bun install/)))).toBe(false);
    expect(readFileSync(join(root, "apps/web/Dockerfile"), "utf8")).toContain("--frozen-lockfile");
    expect(readFileSync(join(root, "services/appview/Dockerfile.standalone"), "utf8")).not.toContain("bun install");
    expect(ci.jobs.web.steps.some((step: any) => step.run === "bash scripts/ci/verify-runtime-images.sh web")).toBe(true);
    expect(ci.jobs.appview_image.steps.some((step: any) => step.run?.includes("bash scripts/ci/verify-runtime-images.sh appview"))).toBe(true);
  });
  it("runs hermetic retry tests in the always-required changes job", () => {
    expect(ci.jobs.changes.steps.some((step: any) => step.run === "node --test scripts/ci/install-bun.test.mjs")).toBe(true);
    expect(ci.jobs.required.needs).toContain("changes");
  });
});
