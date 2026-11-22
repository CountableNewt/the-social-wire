import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const read = (path: string) => readFileSync(join(root, path), "utf8");

test("maintenance jobs use dedicated authentication and matching operational requests", () => {
  const document = Bun.YAML.parse(read("packages/spec/openapi.yaml")) as any;
  const manifest = JSON.parse(read("packages/spec/endpoint-manifest.json")) as { entries: { path: string; method: string; classification: string }[] };
  for (const name of ["scheduling", "drafts-cleanup", "discovery", "ttl-cleanup", "rss-poll", "analytics-cleanup", "author-history", "read-state-recovery"]) {
    for (const [surface, method, scheme, file, secret] of [["jobs", "POST", "MaintenanceJobsBearer", "Run", "jobsSecret"], ["cron", "GET", "MaintenanceCronBearer", "Cron", "cronSecret"]]) {
      const path = `/internal/${surface}/${name}`;
      const operation = document.paths[path][method.toLowerCase()];
      expect(operation.security).toEqual([{ [scheme]: [] }]);
      expect(operation.responses["401"]).toBeDefined();
      expect(operation.responses["503"]).toBeDefined();
      expect(manifest.entries.find(entry => entry.path === path && entry.method === method)?.classification).toBe("operational");
      const bruno = read(`services/appview/bruno/Jobs/${file} ${name}.bru`);
      expect(bruno).toContain(path);
      expect(bruno).toContain(`Authorization: Bearer {{${secret}}}`);
      expect(bruno).not.toContain("oauthAccessToken");
    }
  }
});
