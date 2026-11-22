import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../..");
const backend = readFileSync(join(root, "services/appview/Dockerfile.standalone"), "utf8");
const entrypoint = readFileSync(join(root, "services/appview/bin/standalone-entrypoint"), "utf8");

describe("portable local and recovery images", () => {
  it("packages the pure-Go backend and verified SQLite recovery commands", () => {
    for (const [command, output] of [["appview", "AppView"], ["sqlite-backup", "sqlite-backup"], ["database-preflight", "database-preflight"]]) {
      expect(backend).toContain(`-o /out/${output} ./cmd/${command}`);
      expect(backend).toContain(`COPY --from=builder /out/${output} /usr/local/bin/${output}`);
    }
    expect(backend).toContain("CGO_ENABLED=0 GOOS=linux go build");
    expect(backend).toContain('ENTRYPOINT ["/usr/bin/tini", "-g", "--", "/usr/local/bin/standalone-entrypoint"]');
    expect(backend).toContain('CMD ["/usr/local/bin/AppView"]');
    expect(backend).toContain("useradd --system --no-create-home appuser");
    expect(backend).not.toMatch(/swift|ffmpeg|oven\/bun/i);
  });

  it("retains local volume safety and non-root execution without requiring a remote volume", () => {
    expect(entrypoint).toContain('case "${DATABASE_BACKEND:-sqlite}" in');
    expect(entrypoint).toContain('exec runuser -u appuser -- "$@"');
    expect(entrypoint).toContain('exec runuser -u appuser -- "$0" "$@"');
    expect(entrypoint).toContain("mountpoint -q /data");
    expect(entrypoint).toContain("Refusing a symlinked database file.");
    expect(entrypoint).toContain("chmod 0600");
    expect(entrypoint.indexOf("turso)")).toBeLessThan(entrypoint.indexOf("mountpoint -q /data"));
  });

  it("retains the standalone non-root Web image verified by CI", () => {
    const web = readFileSync(join(root, "apps/web/Dockerfile"), "utf8");
    expect(web).toContain('CMD ["node", "apps/web/server.js"]');
    expect(web).toContain("USER node");
  });
});
