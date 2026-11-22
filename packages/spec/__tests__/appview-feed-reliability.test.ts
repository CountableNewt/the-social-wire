import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dir, "../../..");
const openapi = readFileSync(resolve(root, "packages/spec/openapi.yaml"), "utf8");

describe("AppView feed reliability contract", () => {
  it("requires authoritative read state and typed retry diagnostics", () => {
    expect(openapi).toContain("required: [entryId, title, publishedAt, isRead]");
    expect(openapi).toContain("required: [error, message, requestId, retryable]");
    expect(openapi).toContain(
      'schema: { $ref: "#/components/schemas/AppViewFeedError" }'
    );
  });

  it("documents confirmed tuple boundaries for mark-all-read", () => {
    expect(openapi).toContain(
      "required: [marked, confirmedAt, boundaries, unreadCounts]"
    );
    expect(openapi).toContain("required: [publicationId, createdAt]");
  });

  it("keeps tuple floors and explicit unread overrides in the Go schema", () => {
    const schema = readFileSync(resolve(root, "services/appview/internal/thinstore/migrate.go"), "utf8");
    expect(schema).toContain("read_floor_uri");
    expect(schema).toContain("CREATE TABLE IF NOT EXISTS appview_unread_overrides");
  });
});
