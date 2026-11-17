import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const appDir = join(import.meta.dir, "..", "app");
const globalsCss = readFileSync(join(appDir, "globals.css"), "utf8");
const articleTypographyCss = readFileSync(join(appDir, "articleTypography.css"), "utf8");

function ruleBody(css: string, selector: string): string {
  const start = css.indexOf(`${selector} {`);
  expect(start).toBeGreaterThanOrEqual(0);
  return css.slice(start, css.indexOf("}", start));
}

function declaredWeight(body: string, property: string): number {
  const match = body.match(new RegExp(`${property}:\\s*(\\d+);`));
  expect(match).not.toBeNull();
  return Number(match?.[1]);
}

describe("Bold Text typography", () => {
  const boldRule = ruleBody(globalsCss, 'html[data-bold-text="true"]');

  it("makes body text bold", () => {
    expect(declaredWeight(boldRule, "--app-font-weight")).toBe(700);
  });

  it("keeps heading and title weights heavier than bold body text", () => {
    const bodyWeight = declaredWeight(boldRule, "--app-font-weight");

    expect(declaredWeight(boldRule, "--font-weight-normal")).toBe(bodyWeight);
    expect(declaredWeight(boldRule, "--font-weight-medium")).toBe(bodyWeight);
    expect(declaredWeight(boldRule, "--font-weight-semibold")).toBeGreaterThan(bodyWeight);
    expect(declaredWeight(boldRule, "--font-weight-bold")).toBeGreaterThan(
      declaredWeight(boldRule, "--font-weight-semibold"),
    );
    expect(declaredWeight(boldRule, "--app-heading-font-weight")).toBeGreaterThan(bodyWeight);
  });

  it("routes article heading weights through the Bold Text variable", () => {
    expect(globalsCss).not.toMatch(/(^|[\s;{])font-weight:\s*\d+;/m);
    expect(articleTypographyCss).not.toMatch(/(^|[\s;{])font-weight:\s*\d+;/m);
    expect(
      ruleBody(articleTypographyCss, ".article-body :is(h1, h2, h3, h4, h5, h6)"),
    ).toContain("font-weight: var(--app-heading-font-weight);");
  });
});
