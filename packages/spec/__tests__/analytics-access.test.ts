import { expect, it } from "bun:test";
import { readFileSync } from "node:fs";
const spec = Bun.YAML.parse(readFileSync(new URL("../openapi.yaml", import.meta.url), "utf8"));
it("documents authenticated analytics without a subscription offering", () => {
  expect(spec.paths["/v1/subscription/entitlements"]).toBeUndefined();
  for (const [path, method] of [["/v1/analytics/summary", "get"], ["/v1/analytics/refresh", "post"], ["/v1/analytics/bluesky", "get"], ["/v1/analytics/publications", "get"]]) {
    expect(spec.paths[path][method].security).toEqual([{ ATProtoOAuthDPoP: [] }]);
    expect(spec.paths[path][method].responses["403"]).toBeUndefined();
  }
});
