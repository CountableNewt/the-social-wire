import { afterEach, beforeEach, describe, expect, it, spyOn } from "bun:test";
import { QueryClient, QueryClientProvider, type InfiniteData } from "@tanstack/react-query";
import { act, cleanup, fireEvent, renderHook, waitFor } from "@testing-library/react";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import type { ReactNode } from "react";

import * as Auth from "@/hooks/useAuth";
import { useEntries, useAggregateFeedEntries, type EntriesPage } from "@/hooks/useEntries";
import { useProactiveFeedRefresh } from "@/hooks/useProactiveFeedRefresh";
import * as AppView from "@/lib/thinAppViewClient";
import * as Telemetry from "@/lib/clientPerformanceTelemetry";

const viewerDid = "did:plc:scroll-guard";
const publicationId = "at://did:plc:author/site.standard.publication/main";
const oauth = { did: viewerDid } as unknown as OAuthSession;
const getOAuthSession = () => oauth;
const restores: (() => void)[] = [];
let client: QueryClient;

function page(id: string): EntriesPage {
  return { entries: [{ entryId: id, title: id, publishedAt: "2026-11-07T00:00:00Z", isRead: false }] };
}

function usePublicationFeed(canRefresh: () => boolean) {
  const query = useEntries(publicationId, "unread", canRefresh);
  useProactiveFeedRefresh(publicationId, "unread", true, query.refreshInBackground);
  return query;
}

function useSubscribedFeed(canRefresh: () => boolean) {
  return useAggregateFeedEntries({ kind: "subscribed" }, "unread", canRefresh);
}

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const auth = spyOn(Auth, "useAuth").mockReturnValue({
    session: { did: viewerDid }, getOAuthSession, oauthSessionReloadSeq: 0,
  } as unknown as ReturnType<typeof Auth.useAuth>);
  const enabled = spyOn(AppView, "isThinAppViewEnabled").mockReturnValue(true);
  const counts = spyOn(AppView, "fetchAppViewUnreadCounts").mockResolvedValue({ counts: {} });
  const telemetry = spyOn(Telemetry, "recordClientPerformance").mockResolvedValue(new Response(null, { status: 202 }));
  const visibility = Object.getOwnPropertyDescriptor(document, "visibilityState");
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" });
  restores.push(() => auth.mockRestore(), () => enabled.mockRestore(), () => counts.mockRestore(), () => telemetry.mockRestore(), () => {
    if (visibility) Object.defineProperty(document, "visibilityState", visibility);
    else Reflect.deleteProperty(document, "visibilityState");
  });
});

afterEach(() => {
  cleanup();
  client.clear();
  restores.splice(0).reverse().forEach(restore => restore());
});

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

for (const scope of [
  { name: "publication", key: ["entries", viewerDid, publicationId, "unread"], useFeed: usePublicationFeed },
  { name: "subscribed", key: ["aggregateEntries", viewerDid, "subscribed", "", "unread"], useFeed: useSubscribedFeed },
]) {
  describe(`${scope.name} passive refresh scroll guard`, () => {
    function seed() {
      client.setQueryData<InfiniteData<EntriesPage>>(scope.key, {
        pages: [{ ...page("cached-head"), cursor: "older" }, page("visible-tail")],
        pageParams: [undefined, "older"],
      });
      return client.getQueryData<InfiniteData<EntriesPage>>(scope.key);
    }

    it("does not fetch on focus while the reader is scrolled", async () => {
      const cached = seed();
      const fetch = spyOn(AppView, "listAggregateFeedFromAppView").mockResolvedValue(page("fresh"));
      restores.push(() => fetch.mockRestore());
      let atTop = false;
      renderHook(() => scope.useFeed(() => atTop), { wrapper });
      await act(async () => { fireEvent.focus(window); });
      expect(fetch).not.toHaveBeenCalled();
      expect(client.getQueryData<InfiniteData<EntriesPage>>(scope.key)).toBe(cached);
      atTop = true;
      await act(async () => { fireEvent.focus(window); });
      await waitFor(() => expect(client.getQueryData<InfiniteData<EntriesPage>>(scope.key)?.pages).toEqual([page("fresh")]));
      expect(fetch).toHaveBeenCalledTimes(1);
    });

    it("retains loaded pages without an error if the reader scrolls during a passive request", async () => {
      const cached = seed();
      const updatedAt = client.getQueryState(scope.key)?.dataUpdatedAt;
      let resolve!: (value: EntriesPage) => void;
      const response = new Promise<EntriesPage>(done => { resolve = done; });
      const fetch = spyOn(AppView, "listAggregateFeedFromAppView").mockReturnValue(response);
      restores.push(() => fetch.mockRestore());
      let atTop = true;
      const { result } = renderHook(() => scope.useFeed(() => atTop), { wrapper });
      await act(async () => { fireEvent.focus(window); });
      await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
      atTop = false;
      await act(async () => { resolve(page("fresh")); await response; });
      await waitFor(() => expect(result.current.isFetching).toBe(false));
      expect(client.getQueryData<InfiniteData<EntriesPage>>(scope.key)).toBe(cached);
      expect(client.getQueryState(scope.key)?.dataUpdatedAt).toBe(updatedAt);
      expect(result.current.isError).toBe(false);
      expect(result.current.error).toBeNull();
      atTop = true;
      fetch.mockResolvedValue(page("fresh-after-return"));
      await act(async () => { fireEvent.focus(window); });
      await waitFor(() => expect(client.getQueryData<InfiniteData<EntriesPage>>(scope.key)?.pages).toEqual([page("fresh-after-return")]));
      expect(fetch).toHaveBeenCalledTimes(2);
    });

    it("allows an explicit refresh while the reader is scrolled", async () => {
      seed();
      const fetch = spyOn(AppView, "listAggregateFeedFromAppView").mockResolvedValue(page("explicit-fresh"));
      restores.push(() => fetch.mockRestore());
      const { result } = renderHook(() => scope.useFeed(() => false), { wrapper });
      await act(async () => { await result.current.refetch(); });
      expect(fetch).toHaveBeenCalledTimes(1);
      expect(client.getQueryData<InfiniteData<EntriesPage>>(scope.key)?.pages).toEqual([page("explicit-fresh")]);
      expect(result.current.isError).toBe(false);
    });
  });
}
