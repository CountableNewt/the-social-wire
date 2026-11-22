import { expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const read = (path: string) => readFileSync(join(root, path), "utf8");

test("Retired Media projection is absent while Social composer GIF routes remain", () => {
  const document = Bun.YAML.parse(read("packages/spec/openapi.yaml")) as {
    paths: Record<string, unknown>;
  };
  expect(document.paths["/v1/media/project"]).toBeUndefined();
  expect(document.paths["/v1/media/gifs"]).toBeDefined();
  expect(document.paths["/v1/media/gifs/thumbnail"]).toBeDefined();
  const manifest = JSON.parse(read("packages/spec/endpoint-manifest.json")) as {
    entries: { path: string }[];
  };
  expect(manifest.entries.some(entry => entry.path === "/v1/media/project")).toBe(false);
  expect(read("services/appview/cmd/appview/router.go")).not.toContain("internal/media");
  expect(read("services/appview/internal/edge/gif_routes.go")).toContain('"/v1/media/gifs"');
  for (const service of ["appview", "gateway"]) {
    expect(existsSync(join(root, `services/${service}/bruno/Media/Project Media.bru`))).toBe(false);
  }
});
