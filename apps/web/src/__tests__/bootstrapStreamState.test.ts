import { describe, expect, it } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import {
  applySidebarFoldersEvent,
  applyUnreadCountsEvent,
  writeStreamedEntriesPage,
} from "@/lib/bootstrapStreamState";
import type { PublicationSidebarProjection } from "@/lib/publicationProjectionClient";
import { ENTRIES_QUERY_KEY } from "@/hooks/useEntries";

function minimalProjection(
  overrides: Partial<PublicationSidebarProjection> = {}
): PublicationSidebarProjection {
  return {
    viewerDid: "did:plc:viewer",
    folders: [],
    publicationPrefs: [],
    allPublicationRows: [],
    myPublications: [],
    subscribedUnfoldered: [
      {
        publicationId: "rss:https://example.com/feed.xml",
        authorDid: "did:web:skyreader.rss",
        authorHandle: "RSS",
        title: "Example RSS",
        appViewScope: {
          authorDid: "did:web:skyreader.rss",
          publicationAtUri: null,
          publicationScopeAtUris: [],
          publicationSiteUrls: ["https://example.com/feed.xml"],
        },
        unreadCount: 2,
      },
    ],
    followingTabPublications: [],
    enrollAuthorDids: [],
    refreshedAt: new Date().toISOString(),
    unreadCountsByPublicationId: { "rss:https://example.com/feed.xml": 5 },
    ...overrides,
  };
}

describe("writeStreamedEntriesPage", () => {
  it("normalizes publication ids for the entries query cache key", () => {
    const qc = new QueryClient();
    writeStreamedEntriesPage(qc, {
      publicationId: "at://did:plc:author/site.standard.publication/pub1",
      entries: [{ entryId: "at://did:plc:author/site.standard.document/a", title: "A", publishedAt: "2026-01-01T00:00:00.000Z" }],
      cursor: "next",
    });

    const cached = qc.getQueryData([
      ...ENTRIES_QUERY_KEY("at://did:plc:author/site.standard.publication/pub1"),
      "all",
    ]);
    expect(cached).toBeDefined();
  });
});

describe("applySidebarFoldersEvent", () => {
  it("merges folder layout without overwriting unreadCounts from the prior unreadCounts event", () => {
    const base = applyUnreadCountsEvent(minimalProjection(), {
      "rss:https://example.com/feed.xml": 5,
    }, { replacePublicationIds: ["rss:https://example.com/feed.xml"] });

    const merged = applySidebarFoldersEvent(base, {
      folderSections: [],
      allPublicationRows: [
        {
          ...base.subscribedUnfoldered[0]!,
          unreadCount: 2,
        },
      ],
    });

    expect(merged.unreadCountsByPublicationId?.["rss:https://example.com/feed.xml"]).toBe(
      5
    );
  });
});
