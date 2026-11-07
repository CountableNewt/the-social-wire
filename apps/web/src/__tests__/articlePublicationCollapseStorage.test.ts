import { expect, it } from "bun:test";
import { ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY, loadCollapsedArticlePublications, saveCollapsedArticlePublications } from "@/lib/articles/articlePublicationCollapseStorage";

function memoryStorage(initial: Record<string, string> = {}) {
  const store = { ...initial };
  return { store, getItem: (key: string) => store[key] ?? null, setItem: (key: string, value: string) => { store[key] = value; } };
}

it("persists collapsed publications per viewer DID", () => {
  const storage = memoryStorage();
  saveCollapsedArticlePublications(storage, "did:plc:a", ["at://a/pub/1"]);
  saveCollapsedArticlePublications(storage, "did:plc:b", ["at://b/pub/2"]);
  expect(loadCollapsedArticlePublications(storage, "did:plc:a")).toEqual(["at://a/pub/1"]);
  expect(loadCollapsedArticlePublications(storage, "did:plc:b")).toEqual(["at://b/pub/2"]);
  saveCollapsedArticlePublications(storage, "did:plc:a", []);
  expect(JSON.parse(storage.store[ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY])).toEqual({ "did:plc:b": ["at://b/pub/2"] });
});

it("ignores malformed stored values", () => {
  expect(loadCollapsedArticlePublications(memoryStorage({ [ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY]: "not json" }), "did:plc:a")).toEqual([]);
  expect(loadCollapsedArticlePublications(memoryStorage({ [ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY]: JSON.stringify({ "did:plc:a": ["ok", 4] }) }), "did:plc:a")).toEqual(["ok"]);
  const throwing = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
  expect(loadCollapsedArticlePublications(throwing, "did:plc:a")).toEqual([]);
  expect(() => saveCollapsedArticlePublications(throwing, "did:plc:a", ["x"])).not.toThrow();
});
