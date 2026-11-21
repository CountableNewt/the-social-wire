import { afterEach, expect, it, spyOn } from "bun:test";
import { InfiniteQueryObserver, QueryClient } from "@tanstack/react-query";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as stream from "@/lib/feedRefreshStreamClient";
import type { FeedRefreshSelector, FeedRefreshStreamEvent } from "@/lib/feedRefreshStreamClient";
import { readerRefreshRows, readerRefreshSelectors, readerRefreshSourcesKey, refreshPublicationSources } from "@/lib/refreshPublicationSources";
import type { PublicationSidebarProjection, SidebarPublicationRow } from "@/lib/publicationProjectionClient";
import { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";

const oauthSession = {} as OAuthSession;
const row = (author: string, id = author): SidebarPublicationRow => ({ publicationId: id, authorDid: author, authorHandle: author, title: author, discoveredAt: "", appViewScope: { authorDid: author, publicationAtUri: null, publicationScopeAtUris: [], publicationSiteUrls: [] } });
const restores: (() => void)[] = [];
afterEach(() => { while (restores.length) restores.pop()!(); });

const subscribed: FeedRefreshSelector[] = [{ kind: "subscribed" }];
const publication = (publicationId: string, status: "updated" | "checked" | "unavailable", indexed: number, completed: number, total: number): FeedRefreshStreamEvent =>
  ({ kind: "publication", publicationId, status, indexed, completed, total });
/** Replays events per stream call; `gate` lets a test observe state between events. */
function mockStream(script: (selector: FeedRefreshSelector, emit: (event: FeedRefreshStreamEvent) => void) => Promise<void>) {
  const spy = spyOn(stream, "consumeFeedRefreshStream").mockImplementation(async ({ selector, onEvent, signal }) => {
    signal?.throwIfAborted();
    await script(selector, onEvent);
  });
  restores.push(() => spy.mockRestore());
  return spy;
}
function refresh(args: { onBatch?: () => Promise<void>; rows?: SidebarPublicationRow[]; selectors?: FeedRefreshSelector[]; signal?: AbortSignal; onProgress?: (completed: number, total: number) => void } = {}) {
  return refreshPublicationSources({
    oauthSession, rows: args.rows ?? [row("did:plc:one")], selectors: args.selectors ?? subscribed,
    signal: args.signal ?? new AbortController().signal, onBatch: args.onBatch ?? (async () => {}), onProgress: args.onProgress ?? (() => {}),
  });
}
function paintClock() {
  const timers: { callback: () => void; cleared: boolean }[] = [];
  const originalSet = globalThis.setTimeout;
  const originalClear = globalThis.clearTimeout;
  const set = spyOn(globalThis, "setTimeout").mockImplementation(((callback: () => void, delay: number, ...rest: unknown[]) => {
    if (delay !== 1000) return originalSet(callback, delay, ...rest);
    const timer = { callback, cleared: false };
    timers.push(timer);
    return timer as unknown as ReturnType<typeof setTimeout>;
  }) as typeof setTimeout);
  const clear = spyOn(globalThis, "clearTimeout").mockImplementation(((timer: ReturnType<typeof setTimeout>) => {
    const found = timers.find(value => value === timer as unknown);
    if (found) found.cleared = true;
    else originalClear(timer);
  }) as typeof clearTimeout);
  restores.push(() => set.mockRestore(), () => clear.mockRestore());
  return { tick: async () => {
    const timer = timers.find(value => !value.cleared);
    if (!timer) throw new Error("No paint scheduled");
    timer.cleared = true;
    timer.callback();
    for (let i = 0; i < 12; i++) await Promise.resolve();
  } };
}
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>(done => { resolve = done; });
  return { promise, resolve };
}

it("reports per-publication progress and flushes once after the stream completes", async () => {
  mockStream(async (_, emit) => {
    emit({ kind: "started", total: 3 });
    emit(publication("a", "checked", 0, 1, 3));
    emit(publication("b", "updated", 2, 2, 3));
    emit(publication("c", "checked", 0, 3, 3));
    emit({ kind: "done", total: 3, indexed: 2, unavailable: 0 });
  });
  const progress: [number, number][] = [];
  let paints = 0;
  await refresh({ onBatch: async () => { paints++; }, onProgress: (completed, total) => progress.push([completed, total]) });
  expect(progress).toEqual([[0, 0], [0, 3], [1, 3], [2, 3], [3, 3], [3, 3]]);
  expect(paints).toBe(1);
});

it("repaints mid-stream only after a publication indexed new articles", async () => {
  const clock = paintClock();
  const gate = deferred(), landed = deferred();
  mockStream(async (_, emit) => {
    emit({ kind: "started", total: 2 });
    emit(publication("a", "checked", 0, 1, 2));
    await gate.promise;
    emit(publication("b", "updated", 4, 2, 2));
    await landed.promise;
  });
  let paints = 0;
  const done = refresh({ onBatch: async () => { paints++; } });
  await clock.tick();
  expect(paints).toBe(0);
  gate.resolve();
  await Promise.resolve();
  await clock.tick();
  expect(paints).toBe(1);
  landed.resolve();
  await done;
  expect(paints).toBe(2);
});

it("streams each mounted feed and accumulates their totals", async () => {
  const calls = mockStream(async (selector, emit) => {
    emit({ kind: "started", total: 1 });
    emit(publication(selector.kind, "checked", 0, 1, 1));
  });
  const progress: [number, number][] = [];
  await refresh({ selectors: [{ kind: "subscribed" }, { kind: "folder", id: "f" }], onProgress: (c, t) => progress.push([c, t]) });
  expect(calls.mock.calls.map(([args]) => args.selector)).toEqual([{ kind: "subscribed" }, { kind: "folder", id: "f" }]);
  expect(progress.at(-1)).toEqual([2, 2]);
});

it("reports unavailable RSS publishers by host only after the authoritative flush succeeds", async () => {
  const rss = row("did:web:skyreader.rss", "rss:world");
  rss.appViewScope.publicationSiteUrls = ["https://publisher.test/rss"];
  mockStream(async (_, emit) => {
    emit({ kind: "started", total: 1 });
    emit(publication("rss:world", "unavailable", 0, 1, 1));
  });
  let paints = 0;
  const outcome = await refresh({ rows: [rss], onBatch: async () => { paints++; } }).catch(error => error);
  expect(paints).toBe(1);
  expect(outcome).toBeInstanceOf(PublicationSourceRefreshError);
  expect(outcome.hosts).toEqual(["publisher.test"]);
  expect(outcome.unavailablePublicationIds).toEqual(["rss:world"]);
  await expect(refresh({ rows: [rss], onBatch: async () => { throw new Error("feed failed"); } })).rejects.toThrow("feed failed");
});

it("does not present an unavailable non-RSS publication as a publisher warning", async () => {
  mockStream(async (_, emit) => {
    emit({ kind: "started", total: 1 });
    emit(publication("did:plc:one", "unavailable", 0, 1, 1));
  });
  const error = await refresh().catch(error => error);
  expect(error).not.toBeInstanceOf(PublicationSourceRefreshError);
  expect(error.message).toContain("Some publications could not be checked");
});

it("flushes partial articles before surfacing a failed stream, continuing later feeds", async () => {
  const calls = mockStream(async selector => {
    if (selector.kind === "subscribed") throw new Error("Feed refresh failed (503)");
  });
  let paints = 0;
  const error = await refresh({ selectors: [{ kind: "subscribed" }, { kind: "following" }], onBatch: async () => { paints++; } }).catch(error => error);
  expect(calls).toHaveBeenCalledTimes(2);
  expect(paints).toBe(1);
  expect(error.message).toContain("Some publications could not be checked");
});

it("settles a started paint on cancellation and never flushes afterward", async () => {
  const clock = paintClock();
  const controller = new AbortController();
  const paintGate = deferred();
  mockStream(async (_, emit) => {
    emit({ kind: "started", total: 1 });
    emit(publication("a", "updated", 1, 1, 1));
    await new Promise<void>((_, reject) => controller.signal.addEventListener("abort", () => reject(controller.signal.reason)));
  });
  let paints = 0;
  const done = refresh({ signal: controller.signal, onBatch: async () => { paints++; await paintGate.promise; } }).catch(error => error);
  await clock.tick();
  expect(paints).toBe(1);
  controller.abort();
  paintGate.resolve();
  expect((await done).name).toBe("AbortError");
  expect(paints).toBe(1);
});

it("does no work for an already aborted refresh", async () => {
  const calls = mockStream(async () => {});
  const controller = new AbortController();
  controller.abort();
  let paints = 0;
  await expect(refresh({ signal: controller.signal, onBatch: async () => { paints++; } })).rejects.toThrow();
  expect(calls).not.toHaveBeenCalled();
  expect(paints).toBe(0);
});

it("refreshes feeds once without a stream when no feed is mounted", async () => {
  const calls = mockStream(async () => {});
  let paints = 0;
  await refresh({ selectors: [], onBatch: async () => { paints++; } });
  expect(calls).not.toHaveBeenCalled();
  expect(paints).toBe(1);
});

it("maps mounted feed queries to refresh selectors for this viewer only", () => {
  const client = new QueryClient();
  const observe = (queryKey: unknown[]) => new InfiniteQueryObserver(client, { queryKey, queryFn: async () => ({ entries: [] }), initialPageParam: undefined, getNextPageParam: () => undefined, staleTime: Infinity, initialData: { pages: [{ entries: [] }], pageParams: [undefined] } }).subscribe(() => {});
  const unsubscribes = [
    observe(["aggregateEntries", "viewer", "subscribed", "", "unread"]),
    observe(["aggregateEntries", "viewer", "folder", "rkey", "all"]),
    observe(["entries", "viewer", "at://did:plc:a/site.standard.publication/p", "all"]),
    observe(["aggregateEntries", "other", "following", "", "all"]),
  ];
  expect(readerRefreshSelectors(client, "viewer")).toEqual([
    { kind: "subscribed" }, { kind: "folder", id: "rkey" }, { kind: "publication", id: "at://did:plc:a/site.standard.publication/p" },
  ]);
  unsubscribes.forEach(unsubscribe => unsubscribe());
  client.clear();
});

it("selects subscribed folders without enrolling the following graph or another viewer", () => {
  const client = new QueryClient();
  const subscribed = row("did:plc:subscribed"), folder = row("did:plc:folder"), following = row("did:plc:following");
  const projection: PublicationSidebarProjection = { viewerDid: "viewer", folders: [], publicationPrefs: [], allPublicationRows: [subscribed, folder, following], myPublications: [], subscribedUnfoldered: [subscribed], followingTabPublications: [following], folderSections: [{ folderRkey: "folder", folderUri: "at://folder", publications: [folder] }], enrollAuthorDids: [], refreshedAt: "" };
  const observer = new InfiniteQueryObserver(client, { queryKey: ["aggregateEntries", "viewer", "subscribed", "", "unread"], queryFn: async () => ({ entries: [] }), initialPageParam: undefined, getNextPageParam: () => undefined, staleTime: Infinity, initialData: { pages: [{ entries: [] }], pageParams: [undefined] } });
  const unsubscribe = observer.subscribe(() => {});
  expect(readerRefreshRows(client, "viewer", projection).map(row => row.publicationId)).toEqual([subscribed.publicationId, folder.publicationId]);
  expect(readerRefreshRows(client, "other-viewer", projection)).toEqual([]);
  unsubscribe(); client.clear();
});

it("compares normalized source and membership sets without order or duplicate sensitivity", () => {
  const rss = row("did:web:skyreader.rss", "rss");
  rss.appViewScope.publicationSiteUrls = ["https://FEED.test:443/rss", "https://feed.test/rss"];
  const original = readerRefreshSourcesKey([row("did:plc:author", "at://did:plc:author/site.standard.publication/key"), rss]);
  const equivalent = readerRefreshSourcesKey([rss, row("did:plc:author", "at%3A%2F%2Fdid%3Aplc%3Aauthor%2Fsite.standard.publication%2Fkey"), rss]);
  expect(equivalent).toEqual(original);
  expect(original.sources).toEqual(["author:did:plc:author", "feed:https://feed.test/rss"]);
  const changedMembership = readerRefreshSourcesKey([row("did:plc:author", "different"), rss]);
  expect(changedMembership.sources).toEqual(original.sources);
  expect(changedMembership.membership).not.toEqual(original.membership);
  expect(readerRefreshSourcesKey([row("", "empty")]).sources).toEqual([]);
  rss.appViewScope.publicationSiteUrls = ["invalid", "  "];
  expect(readerRefreshSourcesKey([rss]).sources).toEqual(["feed:invalid"]);
});

it("compares publication scope URLs and AT-URIs even when author and membership stay unchanged", () => {
  const original = row("did:plc:author", "publication");
  original.appViewScope = { authorDid: "did:plc:author", publicationAtUri: "at://did:plc:author/site.standard.publication/key", publicationScopeAtUris: ["at://scope/one", "at://scope/two"], publicationSiteUrls: ["https://ONE.test:443", "https://two.test/"] };
  const key = readerRefreshSourcesKey([original]);
  const reordered = { ...original, appViewScope: { ...original.appViewScope, publicationScopeAtUris: ["at://scope/two", "at://scope/one", "at://scope/one"], publicationSiteUrls: ["https://two.test/", "https://one.test/"] } };
  expect(readerRefreshSourcesKey([reordered])).toEqual(key);
  for (const appViewScope of [
    { ...original.appViewScope, publicationAtUri: "at://different" },
    { ...original.appViewScope, publicationScopeAtUris: ["at://different"] },
    { ...original.appViewScope, publicationSiteUrls: ["https://different.test"] },
  ]) {
    const changed = readerRefreshSourcesKey([{ ...original, appViewScope }]);
    expect(changed.membership).toEqual(key.membership);
    expect(changed.sources).toEqual(key.sources);
    expect(changed.scopes).not.toEqual(key.scopes);
  }
});
