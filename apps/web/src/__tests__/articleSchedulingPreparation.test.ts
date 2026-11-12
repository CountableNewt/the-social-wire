import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";
import { POST } from "@/app/xrpc/at.anypub.prepare/route";
import { prepareScheduledArticle } from "../lib/articles/articleSchedulingPreparation";
import type { ArticlePublishInput } from "../lib/articles/articlePublishingTypes";

const scheduledAt = "2099-01-01T12:00:00.000Z";
function input(host: ArticlePublishInput["publication"]["host"] = "markpub"): ArticlePublishInput {
  return { publication: { uri: "at://did:plc:author/site.standard.publication/publication", cid: "publication-cid", name: "Publication", url: "https://example.com", host, record: {} }, title: "Scheduled Article", path: "/my-article", tags: ["test", "test"], markdown: "# A Heading\n\nA paragraph.", recordKey: "3m234567abcde" };
}

describe("scheduled article preparation through Social Wire’s Web route", () => {
  let fetcher: ReturnType<typeof spyOn<typeof globalThis, "fetch">>;
  beforeEach(() => {
    const routeFetch = (target: Parameters<typeof fetch>[0], init: Parameters<typeof fetch>[1]) => {
      expect(target).toBe("/xrpc/at.anypub.prepare");
      expect(init).toMatchObject({ credentials: "omit", redirect: "error", cache: "no-store" });
      return POST(new Request(`https://testing.thesocialwire.app${target}`, init));
    };
    fetcher = spyOn(globalThis, "fetch").mockImplementation(Object.assign(routeFetch, { preconnect: fetch.preconnect }));
  });
  afterEach(() => fetcher.mockRestore());

  test.each(["markpub", "leaflet", "unknown"] as const)("prepares %s content with stable identity and publication timestamps", async host => {
    const prepared = await prepareScheduledArticle(input(host), scheduledAt);
    expect(prepared.publishRkey).toBe("3m234567abcde");
    expect(prepared.url).toBe("https://example.com/my-article");
    expect(prepared.record).toMatchObject({ $type: "site.standard.document", site: input().publication.uri, title: "Scheduled Article", publishedAt: scheduledAt, updatedAt: scheduledAt, path: "/my-article", tags: ["test"], textContent: "A Heading\n\nA paragraph." });
    if (host === "leaflet") {
      expect(prepared.record.content).toEqual({ $type: "pub.leaflet.content", pages: [{ $type: "pub.leaflet.pages.linearDocument", blocks: [
        { $type: "pub.leaflet.pages.linearDocument#block", block: { $type: "pub.leaflet.blocks.header", level: 1, plaintext: "A Heading" } },
        { $type: "pub.leaflet.pages.linearDocument#block", block: { $type: "pub.leaflet.blocks.text", plaintext: "A paragraph." } },
      ] }] });
    } else {
      expect(prepared.record.content).toEqual({ $type: "at.markpub.markdown", flavor: "gfm", text: { $type: "at.markpub.text", markdown: input().markdown } });
    }
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  test("rejects offloaded text without uploading or writing a record", async () => {
    await expect(prepareScheduledArticle({ ...input(), markdown: "a".repeat(103_000) }, scheduledAt)).rejects.toThrow("Deferred Asset Uploads");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

test("rejects unsupported inputs before contacting the preparation route", async () => {
  const fetcher = spyOn(globalThis, "fetch").mockRejectedValue(new Error("Unexpected Request"));
  try {
    for (const candidate of [input("offprint"), input("pckt"), { ...input(), markdown: "![image](https://example.com/image.png)" }, { ...input(), markdown: "article-asset://image" }, { ...input(), markdown: "blob:cid" }, { ...input(), recordKey: undefined }, { ...input(), path: "/../secret" }, { ...input(), existing: { uri: "at://old", cid: "old", record: {} } }]) {
      await expect(prepareScheduledArticle(candidate, scheduledAt)).rejects.toThrow();
    }
    expect(fetcher).not.toHaveBeenCalled();
  } finally {
    fetcher.mockRestore();
  }
});
