import { describe, expect, it } from "bun:test";
import {
  latrFingerprintFromNormalizedUrl,
  normalizeLatrHttpsUrl,
} from "@/lib/latrSavedUrls";

describe("normalizeLatrHttpsUrl", () => {
  it("lowercases host and promotes http to canonical https string", () => {
    expect(normalizeLatrHttpsUrl("HTTP://Example.COM/foo")).toBe(
      "https://example.com/foo"
    );
  });

  it("drops hash fragments", () => {
    expect(normalizeLatrHttpsUrl("https://a.com/x#y")).toBe("https://a.com/x");
  });

  it("removes tracking query params including utm_*", () => {
    expect(
      normalizeLatrHttpsUrl(
        "https://a.com/article?utm_campaign=x&utm_source=y&id=7"
      )
    ).toBe("https://a.com/article?id=7");
  });

  it("returns null for non-http(s) schemes", () => {
    expect(normalizeLatrHttpsUrl("ftp://a.com/x")).toBeNull();
  });
});

describe("latrFingerprintFromNormalizedUrl", () => {
  it("returns a lowercase sha256 hex string", async () => {
    const fp = await latrFingerprintFromNormalizedUrl("https://example.com/a");
    expect(fp).toMatch(/^[a-f0-9]{64}$/);
  });
});
