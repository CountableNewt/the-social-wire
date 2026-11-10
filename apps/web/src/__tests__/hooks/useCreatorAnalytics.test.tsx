import { afterEach, beforeEach, describe, expect, it, spyOn } from "bun:test";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as AuthStorage from "@/lib/auth";
import * as Auth from "@/hooks/useAuth";
import * as Brands from "@/lib/brandAccounts/brandAccountSessions";
import * as API from "@/lib/analytics/creatorAnalyticsClient";
import * as Bluesky from "@/lib/analytics/blueskyAnalyticsClient";
import { useCreatorAnalytics } from "@/hooks/useCreatorAnalytics";

const summary = (): API.AnalyticsSummary => ({ items: [], totals: { engagement: null, tracking: { impressions: 0, views: 0, outboundClicks: 0, ctr: null } }, series: [], coverage: { snapshotStart: null, inventoryComplete: true, trackingScope: "social-wire", platformImpressions: null } });
const report = (): Bluesky.BlueskyAnalyticsSummary => ({ sampledAt: "2026-11-09T12:00:00Z", days: 30, profile: { followers: 10, following: 5, posts: 2 }, activity: { posts: 2, replies: 0, postsPerDay: 2 / 30 }, topPosts: [], shares: [], coverage: { feedAvailable: true, feedComplete: true, sharesAvailable: true, sharesComplete: false, articlesSearched: 0, feedPostsExamined: 2 } });
let viewer: string; let oauth: string; let client: QueryClient;
let bluesky: ReturnType<typeof spyOn<typeof Bluesky, "getBlueskyAnalytics">>;
let legacy: ReturnType<typeof spyOn<typeof API, "getCreatorAnalytics">>;
const restores: (() => void)[] = [];
function Wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={client}>{children}</QueryClientProvider>; }
beforeEach(() => {
  const previousBrandFlag = process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true";
  restores.push(() => { if (previousBrandFlag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = previousBrandFlag; });
  viewer = "did:plc:alice"; oauth = viewer;
  const hint = spyOn(AuthStorage, "getStoredOAuthDid").mockImplementation(() => viewer); restores.push(() => hint.mockRestore());
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  bluesky = spyOn(Bluesky, "getBlueskyAnalytics").mockResolvedValue(report()); restores.push(() => bluesky.mockRestore());
  legacy = spyOn(API, "getCreatorAnalytics").mockResolvedValue(summary()); restores.push(() => legacy.mockRestore());
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: viewer ? { did: viewer } : null, getOAuthSession: () => oauth ? { did: oauth } as OAuthSession : undefined, oauthSessionReloadSeq: 0 } as ReturnType<typeof Auth.useAuth>)); restores.push(() => auth.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });

describe("AppView analytics query isolation", () => {
  it("loads only live AppView data, forwards range/cancellation and never requests legacy summary", async () => {
    const { result } = renderHook(() => useCreatorAnalytics(7), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    expect(bluesky.mock.calls[0][0].did).toBe("did:plc:alice");
    expect(bluesky.mock.calls[0][1]).toMatchObject({ days: 7 });
    expect(bluesky.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal);
    expect(legacy).not.toHaveBeenCalled();
    expect(Object.keys(result.current).sort()).toEqual(["blueskyQuery", "collect", "refresh"]);
  });

  it("does not display prior account or period data", async () => {
    bluesky.mockImplementation(async (session, options) => session.did === "did:plc:alice" && options.days === 7 ? report() : new Promise(() => {}));
    const { result, rerender } = renderHook(({ days }) => useCreatorAnalytics(days), { initialProps: { days: 7 }, wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.data?.profile.followers).toBe(10));
    rerender({ days: 30 }); expect(result.current.blueskyQuery.data).toBeUndefined();
    viewer = "did:plc:bob"; oauth = viewer;
    rerender({ days: 7 }); expect(result.current.blueskyQuery.data).toBeUndefined();
    expect(legacy).not.toHaveBeenCalled();
  });

  it("refreshes AppView share discovery only after collecting creator article URLs", async () => {
    const { result } = renderHook(() => useCreatorAnalytics(30), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    expect(legacy).not.toHaveBeenCalled();
    await act(async () => { await result.current.collect(); });
    await waitFor(() => expect(bluesky.mock.calls.length).toBeGreaterThanOrEqual(2));
    expect(legacy).toHaveBeenCalledTimes(1);
    expect(legacy.mock.calls[0][1]).toEqual({ days: 30, refresh: true });
    expect(client.getQueryCache().findAll({ queryKey: ["creatorAnalytics"] })).toHaveLength(0);
  });

  it("refreshes live AppView counts even when optional article collection fails", async () => {
    legacy.mockRejectedValue(new Error("Article collection unavailable"));
    const { result } = renderHook(() => useCreatorAnalytics(30), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    await act(async () => { await expect(result.current.collect()).rejects.toThrow("Article collection unavailable"); });
    await waitFor(() => expect(bluesky).toHaveBeenCalledTimes(2));
    expect(result.current.blueskyQuery.data?.profile.followers).toBe(10);
    expect(legacy).toHaveBeenCalledTimes(1);
  });

  it("rejects a late refresh after account changes without invalidating the new account", async () => {
    let resolve!: (value: API.AnalyticsSummary) => void;
    legacy.mockImplementation(async () => new Promise(done => { resolve = done; }));
    const { result, rerender } = renderHook(() => useCreatorAnalytics(30), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    let pending!: Promise<void>;
    act(() => { pending = result.current.collect(); });
    await waitFor(() => expect(resolve).toBeDefined());
    viewer = "did:plc:bob"; oauth = viewer; rerender();
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    await act(async () => { resolve(summary()); await expect(pending).rejects.toThrow("account changed"); });
    expect(client.getQueryState(["blueskyAnalytics", viewer, 0, 30, viewer, true])?.isInvalidated).toBe(false);
    expect(bluesky).toHaveBeenCalledTimes(2);
  });

  it("does not request any private report while signed out", async () => {
    viewer = ""; oauth = "";
    const { result } = renderHook(() => useCreatorAnalytics(30), { wrapper: Wrapper });
    expect(result.current.blueskyQuery.fetchStatus).toBe("idle");
    expect(bluesky).not.toHaveBeenCalled();
    expect(legacy).not.toHaveBeenCalled();
  });

  it("uses the linked identity and rejects refreshing after it is unlinked", async () => {
    let linked = true;
    const registry = spyOn(Brands, "listBrandAccounts").mockImplementation(() => linked ? ["did:plc:brand"] : []); restores.push(() => registry.mockRestore());
    const restore = spyOn(Brands, "restoreBrandAccount").mockResolvedValue({ did: "did:plc:brand" } as unknown as OAuthSession); restores.push(() => restore.mockRestore());
    const { result } = renderHook(() => useCreatorAnalytics(30, "did:plc:brand"), { wrapper: Wrapper });
    await waitFor(() => expect(result.current.blueskyQuery.isSuccess).toBe(true));
    expect(bluesky.mock.calls[0][0].did).toBe("did:plc:brand");
    expect(restore).toHaveBeenCalledWith("did:plc:alice", "did:plc:brand");
    linked = false;
    await act(async () => { await expect(result.current.collect()).rejects.toThrow("no longer linked"); });
    expect(legacy).not.toHaveBeenCalled();
  });
});


it("rejects a brand actor when the release flag is off before restoring or fetching", async () => {
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  const registry = spyOn(Brands, "listBrandAccounts").mockReturnValue(["did:plc:brand"]); restores.push(() => registry.mockRestore());
  const restore = spyOn(Brands, "restoreBrandAccount"); restores.push(() => restore.mockRestore());
  const { result } = renderHook(() => useCreatorAnalytics(30, "did:plc:brand"), { wrapper: Wrapper });
  expect(result.current.blueskyQuery.fetchStatus).toBe("idle");
  expect(result.current.blueskyQuery.data).toBeUndefined();
  expect(restore).not.toHaveBeenCalled();
  expect(bluesky).not.toHaveBeenCalled();
  await act(async () => { await expect(result.current.collect()).rejects.toThrow("Brand Accounts Are Not Available"); });
  expect(legacy).not.toHaveBeenCalled();
});

it("hides an already cached brand report when Brand Accounts becomes unavailable", async () => {
  const registry = spyOn(Brands, "listBrandAccounts").mockReturnValue(["did:plc:brand"]); restores.push(() => registry.mockRestore());
  const restore = spyOn(Brands, "restoreBrandAccount").mockResolvedValue({ did: "did:plc:brand" } as unknown as OAuthSession); restores.push(() => restore.mockRestore());
  const { result, rerender } = renderHook(() => useCreatorAnalytics(30, "did:plc:brand"), { wrapper: Wrapper });
  await waitFor(() => expect(result.current.blueskyQuery.data).toBeDefined());
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  rerender();
  expect(result.current.blueskyQuery.data).toBeUndefined();
  expect(result.current.blueskyQuery.fetchStatus).toBe("idle");
  expect(bluesky).toHaveBeenCalledTimes(1);
  expect(restore).toHaveBeenCalledTimes(1);
});
