import { describe, expect, it } from "bun:test";
import { QueryClient } from "@tanstack/react-query";

import { ENTRIES_QUERY_KEY } from "@/hooks/useEntries";
import { PUBLICATION_SIDEBAR_PROJECTION_QUERY_KEY } from "@/hooks/usePublicationSidebarData";
import type { PublicationSidebarProjection } from "@/lib/publicationProjectionClient";
import { applyPublicationUnreadCountDelta } from "@/lib/optimisticUnreadCounts";

const viewerDid = "did:plc:viewer";
const publicationId = "at://did:plc:author/site.standard.publication/main";

function makeProjection(unreadCount: number): PublicationSidebarProjection {
  return {
    viewerDid,
    folders: [],
    publicationPrefs: [],
    allPublicationRows: [
      {
        publicationId,
        subscriptionPublicationId: publicationId,
        authorDid: "did:plc:author",
        authorHandle: "author.test",
        title: "Main",
        discoveredAt: "2026-01-01T00:00:00.000Z",
        appViewScope: {
          authorDid: "did:plc:author",
          publicationAtUri: publicationId,
          publicationScopeAtUris: [publicationId],
          publicationSiteUrls: [],
        },
        unreadCount,
      },
    ],
    myPublications: [],
    subscribedUnfoldered: [],
    followingTabPublications: [],
    enrollAuthorDids: [],
    refreshedAt: "2026-01-01T00:00:00.000Z",
    unreadCountsByPublicationId: { [publicationId]: unreadCount },
  };
}

describe("applyPublicationUnreadCountDelta", () => {
  it("decrements embedded sidebar and appview unread counts", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(
      PUBLICATION_SIDEBAR_PROJECTION_QUERY_KEY(viewerDid),
      makeProjection(3)
    );
    queryClient.setQueryData(["appviewUnreadCounts", viewerDid, ""], {
      [publicationId]: 3,
    });

    applyPublicationUnreadCountDelta(queryClient, viewerDid, publicationId, -1);

    const projection = queryClient.getQueryData<PublicationSidebarProjection>(
      PUBLICATION_SIDEBAR_PROJECTION_QUERY_KEY(viewerDid)
    );
    expect(projection?.allPublicationRows[0]?.unreadCount).toBe(2);
    expect(projection?.unreadCountsByPublicationId?.[publicationId]).toBe(2);

    const appview = queryClient.getQueryData<Record<string, number>>([
      "appviewUnreadCounts",
      viewerDid,
      "",
    ]);
    expect(appview?.[publicationId]).toBe(2);
  });

  it("clamps unread counts at zero", () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(
      PUBLICATION_SIDEBAR_PROJECTION_QUERY_KEY(viewerDid),
      makeProjection(1)
    );

    applyPublicationUnreadCountDelta(queryClient, viewerDid, publicationId, -1);

    const projection = queryClient.getQueryData<PublicationSidebarProjection>(
      PUBLICATION_SIDEBAR_PROJECTION_QUERY_KEY(viewerDid)
    );
    expect(projection?.allPublicationRows[0]?.unreadCount).toBe(0);
    expect(projection?.unreadCountsByPublicationId?.[publicationId]).toBeUndefined();
  });
});
