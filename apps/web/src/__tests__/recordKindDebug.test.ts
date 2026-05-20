import { describe, expect, it } from "bun:test";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import { rssPublicationIdFromNormalizedFeedUrl } from "@/lib/rssFeedCore";
import {
  recordKindFromEntryId,
  recordKindFromLatrSave,
  recordKindFromPublication,
  recordKindFromPubId,
} from "@/lib/recordKindDebug";

describe("recordKindFromPublication", () => {
  it("classifies standard.site publication AT-URIs", () => {
    const pub: DiscoveredPublication = {
      publicationId:
        "at://did:plc:author/site.standard.publication/offprint",
      subscriptionPublicationId:
        "at://did:plc:author/site.standard.publication/offprint",
      authorDid: "did:plc:author",
      authorHandle: "author.test",
      title: "Offprint",
      discoveredAt: "2026-01-01T00:00:00.000Z",
    };
    expect(recordKindFromPublication(pub)).toEqual({
      source: "standard.site",
      collection: "site.standard.publication",
      detail:
        "site.standard.publication · at://did:plc:author/site.standard.publication/offprint",
    });
  });

  it("classifies Skyreader RSS rows", () => {
    const feedUrl = "https://example.com/feed.xml";
    const pub: DiscoveredPublication = {
      publicationId: rssPublicationIdFromNormalizedFeedUrl(feedUrl),
      authorDid: "did:web:skyreader.rss",
      authorHandle: "RSS",
      title: "Example",
      discoveredAt: "2026-01-01T00:00:00.000Z",
    };
    expect(recordKindFromPublication(pub).source).toBe("skyreader.app");
    expect(recordKindFromPublication(pub).collection).toBe(
      "app.skyreader.feed.subscription"
    );
  });
});

describe("recordKindFromPubId", () => {
  it("classifies author aggregate feeds", () => {
    expect(recordKindFromPubId("did:plc:author").source).toBe("standard.site");
    expect(recordKindFromPubId("did:plc:author").collection).toBe(
      "site.standard.graph.subscription"
    );
  });
});

describe("recordKindFromEntryId", () => {
  it("classifies standard.site documents", () => {
    const entryId = "at://did:plc:author/site.standard.document/post1";
    expect(recordKindFromEntryId(entryId)).toMatchObject({
      source: "standard.site",
      collection: "site.standard.document",
    });
  });

  it("classifies L@tr saved items", () => {
    const entryId = "at://did:plc:viewer/com.latr.saved.item/abc";
    expect(recordKindFromEntryId(entryId).source).toBe("L@tr.link");
  });
});

describe("recordKindFromLatrSave", () => {
  it("classifies HTTPS external wrappers", () => {
    expect(
      recordKindFromLatrSave({
        kind: "external",
        normalizedUrl: "https://example.com/a",
        url: "https://example.com/a",
        savedAt: "2026-01-01T00:00:00.000Z",
        externalRkey: "ext",
        itemRkey: "item",
        externalUri: "at://did/com.latr.saved.external/ext",
        itemUri: "at://did/com.latr.saved.item/item",
        subjectUri: "at://did/com.latr.saved.external/ext",
      }).source
    ).toBe("L@tr.link");
  });
});
