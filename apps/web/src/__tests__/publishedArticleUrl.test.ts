import { expect, it } from "bun:test";
import { publishedArticleUrl } from "@/lib/articles/publishedArticleUrl";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";

const publication: ArticlePublication = { uri: "at://did:plc:author/site.standard.publication/main", cid: "publication", name: "Example", url: "https://example.test/journal", host: "markpub", record: {} };
const article = (record: Record<string, unknown>): PublishedArticle => ({ uri: "at://did:plc:author/site.standard.document/story", cid: "article", record });

it("resolves article paths relative to the matching publication", () => {
  expect(publishedArticleUrl(article({ site: publication.uri, path: "story" }), [publication])).toBe("https://example.test/journal/story");
  expect(publishedArticleUrl(article({ site: publication.uri, path: "/story" }), [publication])).toBe("https://example.test/story");
  expect(publishedArticleUrl(article({ site: "at://other/site.standard.publication/main", path: "/story" }), [publication])).toBeUndefined();
});

it("uses explicit public article URLs without requiring publication discovery", () => {
  expect(publishedArticleUrl(article({ url: "https://publisher.test/story?lang=en#section" }), [])).toBe("https://publisher.test/story?lang=en#section");
  expect(publishedArticleUrl(article({ url: "http://publisher.test/story" }), [])).toBe("http://publisher.test/story");
  expect(publishedArticleUrl(article({ site: publication.uri, url: "https://canonical.test/story", path: "/other" }), [publication])).toBe("https://canonical.test/story");
});

it("rejects executable, credential-bearing, malformed, and unresolved URLs", () => {
  for (const url of ["javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "file:///private/story", "https://user:password@publisher.test/story", "https://user@publisher.test/story", "https://", "/relative-without-publication"]) {
    expect(publishedArticleUrl(article({ url }), [])).toBeUndefined();
  }
  expect(publishedArticleUrl(article({}), [publication])).toBeUndefined();
  expect(publishedArticleUrl(article({ url: 42 }), [publication])).toBeUndefined();
  expect(publishedArticleUrl(article({ site: publication.uri, path: "javascript:alert(1)" }), [publication])).toBeUndefined();
});
