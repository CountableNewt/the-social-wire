import { describe, expect, it } from "bun:test";
import { articleEditCapability, canDeletePublishedArticle, publishedArticleMarkdown } from "@/lib/articles/articleEditCapability";
import type { ArticleHost, ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";

const did = "did:plc:viewer";
function publication(host: ArticleHost, url = "https://journal.test"): ArticlePublication {
  return { uri: `at://${did}/site.standard.publication/site`, cid: "pub-cid", name: "Journal", url, host, record: { $type: "site.standard.publication", url } };
}
function article(content: unknown): PublishedArticle {
  return { uri: `at://${did}/site.standard.document/3m4abcdefghij`, cid: "doc-cid", record: { $type: "site.standard.document", content } };
}
const markdown = { $type: "at.markpub.markdown", text: { $type: "at.markpub.text", markdown: "# Hello" } };

describe("articleEditCapability", () => {
  it("allows full editing of inline Markdown articles", () => {
    expect(articleEditCapability(article(markdown), publication("markpub"))).toBe("full");
    expect(articleEditCapability(article(markdown), publication("unknown"))).toBe("full");
  });
  it("limits native and blob-backed bodies to details", () => {
    expect(articleEditCapability(article({ $type: "pub.leaflet.content" }), publication("leaflet"))).toBe("details");
    expect(articleEditCapability(article({ $type: "app.offprint.content" }), publication("offprint"))).toBe("details");
    const blobBacked = { ...markdown, text: { ...markdown.text, textBlob: { $type: "blob" } } };
    expect(publishedArticleMarkdown({ content: blobBacked })).toBeUndefined();
    expect(articleEditCapability(article(blobBacked), publication("markpub"))).toBe("details");
  });
  it("blocks editing for pckt and missing publications but still deletes orphans", () => {
    expect(articleEditCapability(article(markdown), publication("pckt", "https://me.pckt.blog"))).toBe("none");
    expect(articleEditCapability(article(markdown), undefined)).toBe("none");
    expect(canDeletePublishedArticle(publication("pckt", "https://me.pckt.blog"))).toBe(false);
    expect(canDeletePublishedArticle(undefined)).toBe(true);
    expect(canDeletePublishedArticle(publication("leaflet"))).toBe(true);
  });
});
