import { expect, test } from "bun:test";
import { vercelAnalyticsUrl } from "../lib/vercelAnalyticsUrl";

test("analytics retains UI destinations without private query or fragment state", () => {
  expect(vercelAnalyticsUrl("https://thesocialwire.app/articles?draft=private#secret")).toBe("https://thesocialwire.app/articles");
  expect(vercelAnalyticsUrl("https://thesocialwire.app/callback?code=credential&state=session")).toBe("https://thesocialwire.app/callback");
  expect(vercelAnalyticsUrl("https://thesocialwire.app/read/at%3A%2F%2Fdid%3Aplc%3Aviewer?entry=private")).toBe("https://thesocialwire.app/read/[publication]");
  expect(vercelAnalyticsUrl("https://thesocialwire.app/saved")).toBe("https://thesocialwire.app/saved");
});

test("analytics refuses malformed and non-web URLs", () => {
  expect(vercelAnalyticsUrl("invalid")).toBeNull();
  expect(vercelAnalyticsUrl("javascript:secret")).toBeNull();
});
