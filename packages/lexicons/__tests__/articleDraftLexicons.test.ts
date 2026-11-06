import { expect, test } from "bun:test";
import { Lexicons } from "@atproto/lexicon";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

const prefix = "app.thesocialwire.articleDraft.";
const directory = join(import.meta.dir, "../app/thesocialwire/articleDraft");
const documents = readdirSync(directory).filter(name => name.endsWith(".json")).map(name => JSON.parse(readFileSync(join(directory, name), "utf8")));
const socialDefs = JSON.parse(readFileSync(join(import.meta.dir, "../app/thesocialwire/socialDraft/defs.json"), "utf8"));
const lexicons = new Lexicons([...documents, socialDefs]);
const method = (name: string) => documents.find(document => document.id === prefix + name).defs.main;
const id = "00000000-0000-0000-0000-000000000001";
const mutationId = "00000000-0000-0000-0000-000000000002";
const payload = { id, title: "Private Article", markdown: "A private body.", excerpt: "", path: "", tags: ["writing"], publicationUri: "", createdAt: "2026-11-06T00:00:00Z", updatedAt: "2026-11-06T00:00:00Z", assets: [] };
const input = { expectedRevision: 0, mutationId, draft: payload, attachments: [] };

test("article drafts use only authenticated private query/procedure schemas, never PDS records", () => {
  expect(documents.filter(document => document.defs.main).map(document => document.id.slice(prefix.length)).sort()).toEqual(["delete", "get", "list", "put"]);
  for (const document of documents.filter(document => document.defs.main)) {
    expect(["query", "procedure"]).toContain(document.defs.main.type);
    expect(document.defs.main.description).toContain("Authentication required");
    expect(document.defs.main.description).toContain("Never publishes");
    expect(document.defs.main.description).toContain("private, no-store");
    expect(document.defs.main.errors.map((error: { name: string }) => error.name)).toEqual(expect.arrayContaining(["Unauthorized", "Unavailable", "Conflict", "Quota"]));
    expect(JSON.stringify(document)).not.toContain('"owner"');
  }
});

test("save requires revision and immutable mutation identity while retaining article content", () => {
  expect(() => lexicons.assertValidXrpcInput(prefix + "put", input)).not.toThrow();
  expect(() => lexicons.assertValidXrpcInput(prefix + "put", { ...input, expectedRevision: -1 })).toThrow();
  const { mutationId: _mutationId, ...withoutMutation } = input;
  expect(() => lexicons.assertValidXrpcInput(prefix + "put", withoutMutation)).toThrow();
  expect(() => lexicons.assertValidXrpcInput(prefix + "put", { ...input, draft: { ...payload, publicationClaimId: mutationId } })).not.toThrow();
  expect(() => lexicons.assertValidXrpcInput(prefix + "put", { ...input, draft: { ...payload, publicationClaimId: "not-a-uuid" } })).toThrow();
});

test("snapshots accept private asset bindings and deletion tombstones without a body", () => {
  const assetId = "00000000-0000-0000-0000-000000000003";
  const asset = { assetId, name: "cover.png", contentType: "image/png", byteLength: 50, sha256: "0".repeat(64), kind: "photo", state: "ready" };
  const document = { draftId: id, revision: 1, deleted: false, mutationId, updatedAt: payload.updatedAt, draft: payload, attachments: [{ localAssetId: assetId, asset }] };
  expect(() => lexicons.assertValidXrpcOutput(prefix + "get", document)).not.toThrow();
  expect(() => lexicons.assertValidXrpcOutput(prefix + "get", { ...document, revision: 0 })).toThrow();
  const { draft: _draft, ...tombstone } = document;
  expect(() => lexicons.assertValidXrpcOutput(prefix + "delete", { ...tombstone, deleted: true, attachments: [] })).not.toThrow();
  expect(method("list").parameters.properties.limit).toEqual({ type: "integer", minimum: 1, maximum: 20, default: 20 });
  expect(method("list").description).toContain("ascending");
});
