import { describe, expect, it } from "bun:test";
import type { InfiniteData } from "@tanstack/react-query";

import type { EntriesPage } from "@/hooks/useEntries";
import {
  mergeFeedRefreshForFilter,
  mergeVisibleFeedRefresh,
  unreadFeedPlaceholder,
} from "@/lib/feedCacheMerge";

function cachedFeed(): InfiniteData<EntriesPage, string | undefined> {
  return {
    pages: [
      {
        entries: [
          {
            entryId: "at://did:plc:alice/site.standard.document/unread",
            title: "Unread",
            publishedAt: "2026-01-02T00:00:00.000Z",
            isRead: false,
          },
          {
            entryId: "at://did:plc:alice/site.standard.document/read",
            title: "Read",
            publishedAt: "2026-01-01T00:00:00.000Z",
            isRead: true,
          },
        ],
        cursor: "all-cursor",
      },
    ],
    pageParams: [undefined],
  };
}

function doc(title: string, publishedAt: string): EntriesPage["entries"][number] {
  return { entryId: `at://did:plc:alice/site.standard.document/${title}`, title, publishedAt, isRead: false };
}

describe("feed cache filtering", () => {
  it("paints cached unread rows without reusing the All cursor", () => {
    const placeholder = unreadFeedPlaceholder(cachedFeed());

    expect(placeholder?.pages[0].entries.map((entry) => entry.title)).toEqual([
      "Unread",
    ]);
    expect(placeholder?.pages[0].cursor).toBeUndefined();
  });

  it("replaces unread cache pages when the fresh page is the complete unread set", () => {
    const refreshed = mergeVisibleFeedRefresh(
      cachedFeed(),
      { entries: [doc("fresh", "2026-01-03T00:00:00.000Z")], cursor: undefined },
      "unread"
    );

    expect(refreshed.pages).toHaveLength(1);
    expect(refreshed.pages[0].entries.map((entry) => entry.title)).toEqual(["fresh"]);
    expect(refreshed.pages[0].cursor).toBeUndefined();
  });

  it("keeps bootstrap unread snapshots authoritative", () => {
    const refreshed = mergeFeedRefreshForFilter(
      cachedFeed(),
      { entries: [doc("fresh", "2026-01-03T00:00:00.000Z")], cursor: "unread-cursor" },
      "unread"
    );
    expect(refreshed.pages).toEqual([{ entries: [doc("fresh", "2026-01-03T00:00:00.000Z")], cursor: "unread-cursor" }]);
  });

  it("keeps cached unread rows and pages older than the fresh snapshot so scrolling stays put", () => {
    const existing: InfiniteData<EntriesPage, string | undefined> = {
      pages: [
        { entries: [doc("read-elsewhere", "2026-01-05T00:00:00.000Z"), doc("kept", "2026-01-04T00:00:00.000Z"), doc("older", "2026-01-02T00:00:00.000Z")], cursor: "page-2" },
        { entries: [doc("kept", "2026-01-04T00:00:00.000Z"), doc("tail", "2026-01-01T00:00:00.000Z")], cursor: "page-3" },
      ],
      pageParams: [undefined, "page-2"],
    };
    const refreshed = mergeVisibleFeedRefresh(
      existing,
      { entries: [doc("new", "2026-01-06T00:00:00.000Z"), doc("kept", "2026-01-04T00:00:00.000Z")], cursor: "fresh-cursor" },
      "unread"
    );

    expect(refreshed.pages.map((page) => page.entries.map((entry) => entry.title))).toEqual([
      ["new", "kept", "older"],
      ["tail"],
    ]);
    expect(refreshed.pages[0].cursor).toBe("page-2");
    expect(refreshed.pageParams).toEqual([undefined, "page-2"]);
  });
});
