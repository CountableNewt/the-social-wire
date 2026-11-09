import { describe, expect, it } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const text = readFileSync(join(root, ".github/workflows/ci.yml"), "utf8");
const workflow = Bun.YAML.parse(text) as any;
const changes = workflow.jobs.changes;
const steps = changes.steps;

describe("merged commit verification within required CI", () => {
  it("uses the existing main/dev push suite and keeps PR and merge-group verification", () => {
    expect(existsSync(join(root, ".github/workflows/validated-merge.yml"))).toBe(false);
    expect(workflow.on.push).toEqual({ branches: ["main", "dev"] });
    expect(workflow.on.pull_request).toEqual({ branches: ["main", "dev"] });
    expect(workflow.on).toHaveProperty("merge_group");
    expect(workflow.on).toHaveProperty("workflow_dispatch");
    expect(workflow.jobs.required.name).toBe("CI — Required");
    expect(workflow.jobs.required.needs).toEqual(["changes", "web", "appview"]);
    expect(workflow.jobs.required.if).toBe("always()");
  });

  it("grants read-only proof access without introducing a publishing or merge operation", () => {
    expect(workflow.permissions).toEqual({ contents: "read" });
    expect(changes.permissions).toEqual({ contents: "read", "pull-requests": "read", checks: "read", actions: "read" });
    expect(text).not.toMatch(/:\s*write\s*$|railway up|gh pr merge|git push/m);
    const proof = steps.find((step: any) => step.id === "reuse");
    expect(proof.run).toBe("node scripts/ci/reuse-pr-verification.mjs");
    expect(proof.env.GITHUB_TOKEN).toBe("${{ github.token }}");
    expect(proof["continue-on-error"]).toBeUndefined();
    expect(proof.if).toBeUndefined();
    expect(changes.outputs.reuse_web).toBe("${{ steps.reuse.outputs.reuse_web }}");
    expect(changes.outputs.reuse_appview).toBe("${{ steps.reuse.outputs.reuse_appview }}");
  });

  it("records and tests immutable PR evidence before making any reuse decision", () => {
    const receipt = steps.findIndex((step: any) => step.run === "node scripts/ci/validated-merge.mjs --receipt");
    const verifierTest = steps.findIndex((step: any) => step.run === "node --test scripts/ci/validated-merge.test.mjs");
    const reuseTest = steps.findIndex((step: any) => step.run?.includes("scripts/ci/reuse-pr-verification.test.mjs"));
    const proof = steps.findIndex((step: any) => step.id === "reuse");
    expect(receipt).toBeGreaterThan(-1);
    expect(verifierTest).toBeGreaterThan(receipt);
    expect(reuseTest).toBeGreaterThan(receipt);
    expect(proof).toBeGreaterThan(verifierTest);
    expect(proof).toBeGreaterThan(reuseTest);
    expect(steps[reuseTest].run).toContain("scripts/ci/required-checks.test.mjs");
    expect(steps[reuseTest].run).toContain("scripts/ci/reuse-workflow.test.mjs");
    const gate = workflow.jobs.required.steps.find((step: any) => step.name === "Verify path-filtered checks");
    expect(gate.run).toBe("node scripts/ci/required-checks.mjs");
    expect(gate.env.REUSE_WEB).toBe("${{ needs.changes.outputs.reuse_web }}");
    expect(gate.env.REUSE_APPVIEW).toBe("${{ needs.changes.outputs.reuse_appview }}");
  });

  it("always rechecks current Development parity for Production before trying reuse", () => {
    const parity = steps.findIndex((step: any) => step.name === "Require Production changes in Development first");
    expect(parity).toBeGreaterThan(-1);
    expect(parity).toBeLessThan(steps.findIndex((step: any) => step.id === "reuse"));
    expect(steps[parity].if).toContain("github.event_name == 'push' && github.ref == 'refs/heads/main'");
    expect(steps[parity].if).toContain("pull_request.base.ref == 'main'");
    expect(steps[parity].if).toContain("merge_group.base_ref == 'refs/heads/main'");
    expect(steps[parity].if).not.toContain("reuse");
    expect(steps[parity]["continue-on-error"]).toBeUndefined();
    expect(steps[parity].run).toContain("git fetch --no-tags origin dev");
    expect(steps[parity].run).toContain("git show refs/remotes/origin/dev:scripts/ci/development-parity.mjs");
    expect(steps[parity].run).toContain('--development refs/remotes/origin/dev');
  });
});
