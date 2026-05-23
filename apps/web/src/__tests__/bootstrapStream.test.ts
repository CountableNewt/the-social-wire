import { describe, expect, it } from "bun:test";
import { firstUnreadPriorityPublicationId } from "@/lib/bootstrapStreamModels";
import { parseNdjsonLinesForTest } from "@/lib/bootstrapStreamClient";
import {
  applySidebarPriorityEvent,
  applyUnreadCountsEvent,
} from "@/lib/bootstrapStreamState";
import type { PublicationSidebarProjection } from "@/lib/publicationProjectionClient";

const baseProjection = (): PublicationSidebarProjection => ({
  viewerDid: "did:plc:viewer",
  folders: [],
  publicationPrefs: [],
  allPublicationRows: [],
  myPublications: [],
  subscribedUnfoldered: [
    {
      publicationId: "pub-a",
      authorDid: "did:plc:a",
      authorHandle: "a",
      title: "A",
      discoveredAt: "2026-01-01T00:00:00.000Z",
      appViewScope: {
        authorDid: "did:plc:a",
        publicationAtUri: null,
        publicationScopeAtUris: [],
        publicationSiteUrls: [],
      },
    },
  ],
  followingTabPublications: [],
  enrollAuthorDids: [],
  refreshedAt: "2026-01-01T00:00:00.000Z",
});

describe("bootstrapStreamModels", () => {
  it("selects first priority publication with unread count", () => {
    const id = firstUnreadPriorityPublicationId({
      myPublications: [],
      subscribedUnfoldered: baseProjection().subscribedUnfoldered,
      followingTabPublications: [],
      unreadCounts: { "pub-a": 3 },
    });
    expect(id).toBe("pub-a");
  });
});

describe("bootstrapStreamClient", () => {
  it("parses partial and multi-line NDJSON chunks", () => {
    const events = parseNdjsonLinesForTest(
      '{"kind":"sidebarPriority","sidebarPriority":{"viewerDid":"did:plc:viewer","folders":[],"publicationPrefs":[],"allPublicationRows":[],"myPublications":[],"subscribedUnfoldered":[],"followingTabPublications":[],"enrollAuthorDids":[],"refreshedAt":"2026-01-01T00:00:00.000Z"}}\n{"kind":"done","done":{"refreshedAt":"2026-01-01T00:00:00.000Z"}}\n'
    );
    expect(events).toHaveLength(2);
    expect(events[0]?.kind).toBe("sidebarPriority");
    expect(events[1]?.kind).toBe("done");
  });
});

describe("bootstrapStreamState", () => {
  it("merges unread counts into projection rows", () => {
    const projection = applyUnreadCountsEvent(baseProjection(), { "pub-a": 4 });
    expect(projection.subscribedUnfoldered[0]?.unreadCount).toBe(4);
    expect(projection.unreadCountsByPublicationId?.["pub-a"]).toBe(4);
  });

  it("replaces projection on sidebarPriority", () => {
    const next = applySidebarPriorityEvent(undefined, baseProjection());
    expect(next.subscribedUnfoldered).toHaveLength(1);
  });
});
