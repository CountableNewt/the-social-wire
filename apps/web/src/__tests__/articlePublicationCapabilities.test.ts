import { expect, it, spyOn } from "bun:test";
import { articlePublicationDisabledReason, assertArticlePublicationEditable, PCKT_EXTERNAL_EDITORS_MESSAGE } from "@/lib/articles/articlePublicationCapabilities";
import { prepareScheduledArticle } from "@/lib/articles/articleSchedulingPreparation";
import type { ArticlePublication, ArticlePublishInput } from "@/lib/articles/articlePublishingTypes";

const supported: ArticlePublication = { uri: "at://did:plc:viewer/site.standard.publication/site", cid: "pub", name: "Journal", host: "markpub", url: "https://journal.test", record: {} };

it("disables pckt by declared host, either URL, or theme even when another hint says supported", () => {
  for (const publication of [
    { ...supported, host: "pckt" as const },
    { ...supported, url: "https://PCKT.BLOG" },
    { ...supported, record: { url: "https://journal.pckt.blog", theme: { $type: "leaflet.theme" } } },
    { ...supported, record: { theme: { $type: "blog.pckt.theme" } } },
  ]) {
    expect(articlePublicationDisabledReason(publication)).toBe(PCKT_EXTERNAL_EDITORS_MESSAGE);
    expect(() => assertArticlePublicationEditable(publication)).toThrow(PCKT_EXTERNAL_EDITORS_MESSAGE);
  }
  for (const publication of [supported, { ...supported, url: "https://pckt.blog.example.com" }, { ...supported, url: "invalid" }]) expect(articlePublicationDisabledReason(publication)).toBeUndefined();
});

it("rejects pckt scheduling before contacting the preparation service", async () => {
  const fetcher = spyOn(globalThis, "fetch").mockRejectedValue(new Error("Unexpected Request"));
  try {
    const input: ArticlePublishInput = { publication: { ...supported, record: { url: "https://journal.pckt.blog" } }, title: "Article", markdown: "Body", tags: [], path: "/article", recordKey: "3m234567abcde" };
    await expect(prepareScheduledArticle(input, "2099-01-01T00:00:00Z")).rejects.toThrow(PCKT_EXTERNAL_EDITORS_MESSAGE);
    expect(fetcher).not.toHaveBeenCalled();
  } finally { fetcher.mockRestore(); }
});
