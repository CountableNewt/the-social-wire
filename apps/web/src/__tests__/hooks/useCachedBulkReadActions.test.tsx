import { beforeEach, describe, expect, it, mock } from "bun:test";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import React from "react";
import { useCachedBulkReadActions } from "@/hooks/useCachedBulkReadActions";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import { ENTRIES_QUERY_KEY } from "@/hooks/useEntries";

const markEntriesRead = mock(() => {});
const markEntriesUnread = mock(() => {});

mock.module("@/contexts/ReadRouteContext", () => ({
  useReadRoute: () => ({
    markEntriesRead,
    markEntriesUnread,
  }),
}));

mock.module("@/hooks/useAuth", () => ({
  useAuth: () => ({
    getOAuthSession: () => null,
  }),
}));

mock.module("@/hooks/useEntriesCacheEpoch", () => ({
  useEntriesCacheEpoch: () => 0,
}));

const pub: DiscoveredPublication = {
  publicationId: "did:plc:alice",
  subscriptionPublicationId: "did:plc:alice",
  authorDid: "did:plc:alice",
  authorHandle: "alice.test",
  title: "Alice",
  discoveredAt: "2026-01-01T00:00:00.000Z",
};

describe("useCachedBulkReadActions", () => {
  beforeEach(() => {
    markEntriesRead.mockClear();
    markEntriesUnread.mockClear();
  });

  it("disables bulk actions when cache is empty", () => {
    const queryClient = new QueryClient();
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(
      () => useCachedBulkReadActions([pub]),
      { wrapper }
    );

    expect(result.current.bulkDisabled).toBe(true);
    expect(result.current.cachedEntryIds).toEqual([]);
  });

  it("marks cached entries read without per-entry PDS sync", () => {
    const queryClient = new QueryClient();
    const entryId = "at://did:plc:alice/site.standard.document/one";
    queryClient.setQueryData([...ENTRIES_QUERY_KEY(pub.publicationId), "all"], {
      pages: [
        {
          entries: [
            {
              entryId,
              title: "One",
              publishedAt: "2026-01-01T00:00:00.000Z",
            },
          ],
          cursor: undefined,
        },
      ],
      pageParams: [undefined],
    });

    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useCachedBulkReadActions([pub]), {
      wrapper,
    });

    result.current.applyMarkAllRead();

    expect(markEntriesRead).toHaveBeenCalledWith([entryId], {
      publications: [pub],
      syncToPds: false,
    });
  });
});
