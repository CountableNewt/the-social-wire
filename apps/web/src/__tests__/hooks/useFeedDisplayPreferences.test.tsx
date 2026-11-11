import { afterEach, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";

import * as AuthHook from "@/hooks/useAuth";
import * as PDSHook from "@/hooks/usePDSClient";
import * as DummyReaderData from "@/lib/dummyReaderData";
import * as SyncPreferencesClient from "@/lib/syncPreferencesClient";
import * as AccountPreferences from "@/hooks/useReadLaterPreferences";
import { useFeedDisplayPreferences } from "@/hooks/useFeedDisplayPreferences";
import { accountPreferencesQueryKey } from "@/hooks/useReadLaterPreferences";
import { loadCachedFeedDisplayPreferences } from "@/lib/feedPreferences";
import type { PDSClient, PreferencesRecord, RepoRecord } from "@/lib/pdsClient";

const did = "did:plc:feed-display-tests";
const session = { did };
let currentDid = did;
let currentOAuth = session;
const storageKey = `the-social-wire.feed-display.v1:${did}`;
const initial: RepoRecord<PreferencesRecord> = {
  uri: `at://${did}/app.thesocialwire.preferences/self`,
  cid: "previous-cid",
  value: {
    $type: "app.thesocialwire.preferences",
    createdAt: "2026-09-08T12:00:00Z",
    updatedAt: "2026-09-08T12:00:00Z",
    visibleFeeds: ["subscribed", "following"],
    showWire: true,
    showCircle: true,
    feedsWithUnreadCounts: ["subscribed"],
    rssArticleOpenMode: "reader",
  },
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let queryClient: QueryClient;
const restoreSpies: Array<() => void> = [];
const upsertPreferences = mock<PDSClient["upsertPreferences"]>();
const fetchPreferences = mock<typeof SyncPreferencesClient.fetchSyncPreferences>();

function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function useSettingsAndSidebar() {
  return {
    settings: useFeedDisplayPreferences(),
    sidebar: useFeedDisplayPreferences(),
  };
}

describe("feed display preference persistence", () => {
  beforeEach(() => {
    currentDid = did; currentOAuth = session;
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    queryClient.setQueryData(accountPreferencesQueryKey(did), initial);
    window.localStorage.removeItem(storageKey);
    upsertPreferences.mockReset();
    fetchPreferences.mockReset();
    fetchPreferences.mockResolvedValue(initial);
    const auth = spyOn(AuthHook, "useAuth").mockImplementation(() => ({
      session: { did: currentDid },
      getOAuthSession: () => currentOAuth,
    } as ReturnType<typeof AuthHook.useAuth>));
    const pds = spyOn(PDSHook, "usePDSClient").mockReturnValue({
      upsertPreferences,
    } as unknown as PDSClient);
    const dummy = spyOn(DummyReaderData, "isDummyReaderDataEnabled").mockReturnValue(false);
    const sync = spyOn(SyncPreferencesClient, "fetchSyncPreferences").mockImplementation(fetchPreferences);
    restoreSpies.push(() => auth.mockRestore(), () => pds.mockRestore(),
      () => dummy.mockRestore(), () => sync.mockRestore());
  });

  afterEach(() => {
    cleanup();
    queryClient.clear();
    restoreSpies.splice(0).forEach((restore) => restore());
    window.localStorage.removeItem(storageKey);
    for (const bob of ["did:plc:bob", "did:plc:loading-bob", "did:plc:pending-bob", "did:plc:response-bob"]) window.localStorage.removeItem(`the-social-wire.feed-display.v1:${bob}`);
  });

  it("isolates saved automatic-read policy and cache when switching accounts without unmounting", async () => {
    const bob = "did:plc:bob";
    const bobRecord = { ...initial, uri: `at://${bob}/app.thesocialwire.preferences/self`, value: { ...initial.value, automaticallyMarkAsRead: false } };
    queryClient.setQueryData(accountPreferencesQueryKey(bob), bobRecord);
    const { result, rerender } = renderHook(useSettingsAndSidebar, { wrapper });
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(true);
    currentDid = bob; currentOAuth = { did: bob };
    rerender();
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
    expect(result.current.settings.automaticReadReady).toBe(true);
    await waitFor(() => expect(loadCachedFeedDisplayPreferences(window.localStorage, bob)?.automaticallyMarkAsRead).toBe(false));
    expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(accountPreferencesQueryKey(did))).toEqual(initial);
  });

  it("does not treat the previous viewer's cached policy as ready while the next account loads", async () => {
    const bob = "did:plc:loading-bob";
    fetchPreferences.mockReturnValue(new Promise(() => {}));
    window.localStorage.setItem(`the-social-wire.feed-display.v1:${bob}`, JSON.stringify({ automaticallyMarkAsRead: false }));
    const { result, rerender } = renderHook(useSettingsAndSidebar, { wrapper });
    currentDid = bob; currentOAuth = { did: bob };
    rerender();
    expect(result.current.settings.automaticReadReady).toBe(false);
    await waitFor(() => expect(result.current.settings.automaticReadReady).toBe(true));
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, bob)?.automaticallyMarkAsRead).toBe(false);
  });

  it.each(["success", "failure"])("keeps a pending previous-account save from changing the current policy (%s)", async outcome => {
    const bob = "did:plc:pending-bob";
    const bobRecord = { ...initial, uri: `at://${bob}/app.thesocialwire.preferences/self`, value: { ...initial.value, automaticallyMarkAsRead: false } };
    queryClient.setQueryData(accountPreferencesQueryKey(bob), bobRecord);
    const request = deferred<RepoRecord<PreferencesRecord>>();
    upsertPreferences.mockReturnValue(request.promise);
    const { result, rerender } = renderHook(useSettingsAndSidebar, { wrapper });
    act(() => result.current.settings.setHideFinanceCrypto(true));
    await waitFor(() => expect(upsertPreferences).toHaveBeenCalledTimes(1));
    currentDid = bob; currentOAuth = { did: bob };
    rerender();
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
    expect(result.current.settings.isPending).toBe(false);
    await act(async () => {
      if (outcome === "success") request.resolve({ ...initial, value: { ...initial.value, automaticallyMarkAsRead: true, hideFinanceCrypto: true } });
      else request.reject(new Error("Previous account write failed"));
    });
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
    expect(result.current.settings.error).toBeNull();
    expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(accountPreferencesQueryKey(bob))).toEqual(bobRecord);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, bob)?.automaticallyMarkAsRead).toBe(false);
  });

  it("does not install a previous account's late preferences response for a new viewer", async () => {
    queryClient.clear();
    const bob = "did:plc:response-bob";
    const request = deferred<RepoRecord<PreferencesRecord>>();
    const bobRecord = { ...initial, uri: `at://${bob}/app.thesocialwire.preferences/self`, value: { ...initial.value, automaticallyMarkAsRead: false } };
    fetchPreferences.mockImplementation(async (_oauth, viewer) => viewer === did ? request.promise : bobRecord);
    const { result, rerender } = renderHook(useSettingsAndSidebar, { wrapper });
    await waitFor(() => expect(fetchPreferences).toHaveBeenCalledTimes(1));
    currentDid = bob; currentOAuth = { did: bob };
    rerender();
    await waitFor(() => expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false));
    await act(async () => request.resolve(initial));
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
    expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(accountPreferencesQueryKey(bob))).toEqual(bobRecord);
  });

  it("rejects mismatched preference records instead of treating another account's policy as known", () => {
    queryClient.setQueryData(accountPreferencesQueryKey(did), { ...initial, uri: "at://did:plc:other/app.thesocialwire.preferences/self" });
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    expect(result.current.settings.automaticReadReady).toBe(false);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)).toBeNull();
  });

  it.each(["pending", "error"])("does not enable automatic reads before uncached preferences are known (%s)", async status => {
    const account = spyOn(AccountPreferences, "useAccountPreferences").mockReturnValue({ data: undefined, isLoading: status === "pending" } as ReturnType<typeof AccountPreferences.useAccountPreferences>);
    restoreSpies.push(() => account.mockRestore());
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    expect(result.current.settings.automaticReadReady).toBe(false);
    expect(result.current.settings.isPending).toBe(true);
    await act(async () => result.current.settings.setHideFinanceCrypto(true));
    expect(upsertPreferences).not.toHaveBeenCalled();
  });

  it("uses cached manual read mode while a fresh preferences request is pending", async () => {
    const account = spyOn(AccountPreferences, "useAccountPreferences").mockReturnValue({ data: undefined, isLoading: true } as ReturnType<typeof AccountPreferences.useAccountPreferences>);
    restoreSpies.push(() => account.mockRestore());
    window.localStorage.setItem(storageKey, JSON.stringify({ automaticallyMarkAsRead: false }));
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    await waitFor(() => expect(result.current.settings.automaticReadReady).toBe(true));
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
  });

  it("enables the default policy after a successful missing-preferences response", () => {
    const account = spyOn(AccountPreferences, "useAccountPreferences").mockReturnValue({ data: null, isLoading: false } as ReturnType<typeof AccountPreferences.useAccountPreferences>);
    restoreSpies.push(() => account.mockRestore());
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    expect(result.current.settings.automaticReadReady).toBe(true);
    expect(result.current.settings.preferences.automaticallyMarkAsRead).toBe(true);
  });

  it("persists manual read mode, shares it with the reader, and keeps it after remount", async () => {
    const saved = { ...initial, cid: "manual-read", value: { ...initial.value, automaticallyMarkAsRead: false } };
    upsertPreferences.mockResolvedValue(saved);
    fetchPreferences.mockResolvedValue(saved);
    const first = renderHook(useSettingsAndSidebar, { wrapper });
    expect(first.result.current.settings.preferences.automaticallyMarkAsRead).toBe(true);
    act(() => first.result.current.settings.setAutomaticallyMarkAsRead(false));
    await waitFor(() => expect(first.result.current.settings.isPending).toBe(false));
    await waitFor(() => expect(upsertPreferences).toHaveBeenCalledTimes(1));
    expect(upsertPreferences.mock.calls[0]![0]).toMatchObject({ automaticallyMarkAsRead: false });
    expect(first.result.current.sidebar.preferences.automaticallyMarkAsRead).toBe(false);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.automaticallyMarkAsRead).toBe(false);
    first.unmount();
    const second = renderHook(useSettingsAndSidebar, { wrapper });
    expect(second.result.current.settings.preferences.automaticallyMarkAsRead).toBe(false);
  });

  it("rolls back a failed automatic read preference save across observers and cache", async () => {
    const request = deferred<RepoRecord<PreferencesRecord>>();
    upsertPreferences.mockReturnValue(request.promise);
    fetchPreferences.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    act(() => result.current.settings.setAutomaticallyMarkAsRead(false));
    await waitFor(() => expect(result.current.sidebar.preferences.automaticallyMarkAsRead).toBe(false));
    await act(async () => request.reject(new Error("PDS save failed")));
    await waitFor(() => expect(result.current.settings.isPending).toBe(false));
    expect(result.current.sidebar.preferences.automaticallyMarkAsRead).toBe(true);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.automaticallyMarkAsRead).toBe(true);
  });

  it("persists crypto hiding and rolls back an unsuccessful optimistic save", async () => {
    const request = deferred<RepoRecord<PreferencesRecord>>();
    upsertPreferences.mockReturnValue(request.promise);
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    act(() => result.current.settings.setHideFinanceCrypto(true));
    await waitFor(() => expect(result.current.sidebar.preferences.hideFinanceCrypto).toBe(true));
    expect(upsertPreferences.mock.calls[0]![0]).toMatchObject({ hideFinanceCrypto: true, showFinance: true });
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.hideFinanceCrypto).toBe(true);
    await act(async () => request.reject(new Error("Failed Save")));
    await waitFor(() => expect(result.current.settings.isPending).toBe(false));
    expect(result.current.sidebar.preferences.hideFinanceCrypto).toBe(false);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.hideFinanceCrypto).toBe(false);
  });

  for (const feed of ["subscribed", "following"] as const) {
    it(`ignores unread-count changes for explicitly hidden ${feed}`, async () => {
      const hidden = { ...initial, value: { ...initial.value, visibleFeeds: ["readLater"] as const, feedsWithUnreadCounts: [] } };
      queryClient.setQueryData(accountPreferencesQueryKey(did), hidden);
      const { result } = renderHook(useSettingsAndSidebar, { wrapper });
      act(() => result.current.settings.setFeedUnreadCountVisible(feed, true));
      expect(upsertPreferences).not.toHaveBeenCalled();
      expect(result.current.sidebar.preferences.feedsWithUnreadCounts).toEqual([]);
    });
  }

  it("saves hiding the final top-level feed and retains the explicit empty array", async () => {
    const only = { ...initial, value: { ...initial.value, visibleFeeds: ["subscribed"] as ["subscribed"] } };
    queryClient.setQueryData(accountPreferencesQueryKey(did), only);
    const saved = { ...only, value: { ...only.value, visibleFeeds: [], feedsWithUnreadCounts: [] } };
    upsertPreferences.mockResolvedValue(saved);
    fetchPreferences.mockResolvedValue(saved);
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    act(() => result.current.settings.setFeedVisible("subscribed", false));
    await waitFor(() => expect(upsertPreferences).toHaveBeenCalledTimes(1));
    expect(upsertPreferences.mock.calls[0]![0]).toMatchObject({ visibleFeeds: [], feedsWithUnreadCounts: [] });
    await waitFor(() => expect(result.current.settings.isPending).toBe(false));
    expect(result.current.sidebar.preferences.visibleFeeds).toEqual([]);
    expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.visibleFeeds).toEqual([]);
  });

  it.each(["wire", "circle"] as const)("ignores attempts to reactivate retired %s preferences", feed => {
    const { result } = renderHook(useSettingsAndSidebar, { wrapper });
    act(() => result.current.settings.setDiscoveryFeedVisible(feed, true));
    expect(upsertPreferences).not.toHaveBeenCalled();
    expect(result.current.sidebar.preferences.showWire).toBe(false);
    expect(result.current.sidebar.preferences.showCircle).toBe(false);
    expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(accountPreferencesQueryKey(did))).toEqual(initial);
  });

  for (const feed of ["social", "podcasts"] as const) {
    const field = ({ wire: "showWire", circle: "showCircle", social: "showSocial", podcasts: "showPodcasts" } as const)[feed];
    const otherField = "showWire";

    it(`persists hiding ${feed} and shares the committed record with another observer`, async () => {
      const request = deferred<RepoRecord<PreferencesRecord>>();
      upsertPreferences.mockReturnValue(request.promise);
      const { result } = renderHook(useSettingsAndSidebar, { wrapper });

      act(() => result.current.settings.setDiscoveryFeedVisible(feed, false));
      await waitFor(() => {
        expect(upsertPreferences).toHaveBeenCalledTimes(1);
        expect(result.current.settings.isPending).toBe(true);
        expect(result.current.sidebar.preferences[field]).toBe(false);
      });
      expect(upsertPreferences.mock.calls[0]![0]).toMatchObject({
        [field]: false,
        [otherField]: false,
        visibleFeeds: initial.value.visibleFeeds,
        feedsWithUnreadCounts: initial.value.feedsWithUnreadCounts,
        rssArticleOpenMode: "reader",
      });
      expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.[field]).toBe(false);

      const saved = {
        ...initial,
        cid: "committed-cid",
        value: { ...initial.value, [field]: false },
      };
      await act(async () => request.resolve(saved));
      await waitFor(() => expect(result.current.settings.isPending).toBe(false));
      expect(result.current.settings.preferences[field]).toBe(false);
      expect(result.current.sidebar.preferences[field]).toBe(false);
      expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(
        accountPreferencesQueryKey(did),
      )).toEqual(saved);
      expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.[field]).toBe(false);
      expect(fetchPreferences).toHaveBeenCalledWith(expect.anything(), did, undefined, true);
    });

    it(`refreshes the Gateway after saving ${feed} before a new reader mounts`, async () => {
      const saved = { ...initial, cid: "new-cid", value: { ...initial.value, [field]: false } };
      let gatewayRecord = initial;
      upsertPreferences.mockResolvedValue(saved);
      fetchPreferences.mockImplementation(async (_oauth, _did, _etag, fresh) => {
        if (fresh) gatewayRecord = saved;
        return gatewayRecord;
      });
      const first = renderHook(useSettingsAndSidebar, { wrapper });
      act(() => first.result.current.settings.setDiscoveryFeedVisible(feed, false));
      await waitFor(() => expect(first.result.current.settings.isPending).toBe(false));
      await waitFor(() => expect(gatewayRecord).toEqual(saved));
      first.unmount();
      queryClient.clear();
      const reloaded = renderHook(useSettingsAndSidebar, { wrapper });
      await waitFor(() => expect(fetchPreferences).toHaveBeenCalledTimes(2));
      await waitFor(() => expect(reloaded.result.current.sidebar.preferences[field]).toBe(false));
    });

    it(`retains the committed ${feed} preference if the Gateway refresh fails`, async () => {
      const saved = { ...initial, cid: "new-cid", value: { ...initial.value, [field]: false } };
      upsertPreferences.mockResolvedValue(saved);
      fetchPreferences.mockRejectedValue(new Error("Gateway unavailable"));
      const { result } = renderHook(useSettingsAndSidebar, { wrapper });
      act(() => result.current.settings.setDiscoveryFeedVisible(feed, false));
      await waitFor(() => expect(fetchPreferences).toHaveBeenCalledTimes(1));
      await waitFor(() => expect(result.current.settings.isPending).toBe(false));
      expect(result.current.sidebar.preferences[field]).toBe(false);
      expect(result.current.settings.error).toBeNull();
    });

    it(`rolls back hiding ${feed} in both observers and local storage when saving fails`, async () => {
      const request = deferred<RepoRecord<PreferencesRecord>>();
      upsertPreferences.mockReturnValue(request.promise);
      // The rollback must work immediately, even while the subsequent refresh is unavailable.
      fetchPreferences.mockReturnValue(new Promise(() => {}));
      const { result } = renderHook(useSettingsAndSidebar, { wrapper });

      act(() => result.current.settings.setDiscoveryFeedVisible(feed, false));
      await waitFor(() => {
        expect(upsertPreferences).toHaveBeenCalledTimes(1);
        expect(result.current.sidebar.preferences[field]).toBe(false);
        expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.[field]).toBe(false);
      });

      const error = new Error("PDS write failed");
      await act(async () => request.reject(error));
      await waitFor(() => {
        expect(result.current.settings.error).toBe(error);
        expect(result.current.settings.preferences[field]).toBe(true);
        expect(result.current.sidebar.preferences[field]).toBe(true);
        expect(fetchPreferences).toHaveBeenCalledTimes(1);
      });
      expect(queryClient.getQueryData<RepoRecord<PreferencesRecord>>(
        accountPreferencesQueryKey(did),
      )).toEqual(initial);
      expect(loadCachedFeedDisplayPreferences(window.localStorage, did)?.[field]).toBe(true);
      expect(result.current.sidebar.preferences[otherField]).toBe(false);
    });
  }
  it("restores visible Sports scores in PDS and refreshes the Gateway before settling",async()=>{
    const hidden={...initial,value:{...initial.value,hideSportsScores:true}};
    const saved={...hidden,cid:"scores-visible",value:{...hidden.value,hideSportsScores:false}};
    queryClient.setQueryData(accountPreferencesQueryKey(did),hidden);
    upsertPreferences.mockResolvedValue(saved);
    const refreshed=deferred<RepoRecord<PreferencesRecord>>();
    fetchPreferences.mockReturnValue(refreshed.promise);
    const {result}=renderHook(useSettingsAndSidebar,{wrapper});
    act(()=>result.current.settings.setHideSportsScores(false));
    await waitFor(()=>expect(fetchPreferences).toHaveBeenCalledTimes(1));
    expect(upsertPreferences.mock.calls[0]?.[0].hideSportsScores).toBe(false);
    expect(result.current.sidebar.preferences.hideSportsScores).toBe(false);
    expect(result.current.settings.isPending).toBe(true);
    await act(async()=>refreshed.resolve(saved));
    await waitFor(()=>expect(result.current.settings.isPending).toBe(false));
    expect(result.current.sidebar.preferences.hideSportsScores).toBe(false);
    expect(result.current.settings.error).toBeNull();
  });

});
