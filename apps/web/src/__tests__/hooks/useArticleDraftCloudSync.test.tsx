import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, renderHook, waitFor } from "@testing-library/react";
import { IDBFactory } from "fake-indexeddb";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Cloud from "@/lib/articles/articleDraftCloudClient";
import type { ArticleDraftDocument } from "@/lib/articles/articleDraftCloudTypes";
import * as Sync from "@/lib/articles/articleDraftSync";
import * as Storage from "@/lib/articles/articleDraftStorage";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticleDraftActor } from "@/lib/articles/articleDraftSync";
import { useArticleDrafts } from "@/hooks/articles/useArticleDrafts";
const realSyncOne = Sync.syncArticleDraft, realSyncAll = Sync.syncArticleDrafts, realLoad = Sync.loadSyncedArticleDraft;
function realSync() {
  const one = spyOn(Sync, "syncArticleDraft").mockImplementation(realSyncOne); restores.push(() => one.mockRestore());
  const all = spyOn(Sync, "syncArticleDrafts").mockImplementation(realSyncAll); restores.push(() => all.mockRestore());
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockImplementation(realLoad); restores.push(() => load.mockRestore());
}
function cloudReply(value: ArticleDraft): ArticleDraftDocument {
  const { revision: _revision, ...payload } = value; void _revision;
  return { draftId: value.id, revision: 1, deleted: false, mutationId: crypto.randomUUID(), updatedAt: value.updatedAt, draft: { ...payload, assets: payload.assets ?? [] }, attachments: [] };
}
function abortable<T>(context: ArticleDraftActor, promise: Promise<T>): Promise<T> {
  if (!context.signal) return promise;
  const signal = context.signal;
  return new Promise((resolve, reject) => {
    const abort = () => reject(signal.reason);
    if (signal.aborted) { abort(); return; }
    signal.addEventListener("abort", abort, { once: true });
    promise.then(value => { signal.removeEventListener("abort", abort); resolve(value); }, error => { signal.removeEventListener("abort", abort); reject(error); });
  });
}
const did = "did:plc:alice", id = "11111111-1111-4111-8111-111111111111";
const session = { did } as unknown as OAuthSession;
const actor = (): ArticleDraftActor => ({ session, viewerDid: did });
const restores: (() => void)[] = [];
const originalIDB = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
function draft(): ArticleDraft { return { id, title: "Legacy Private Draft", markdown: "Original", excerpt: "", path: "", tags: [], publicationUri: "", createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-01T00:00:00Z", assets: [] }; }
beforeEach(async () => {
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() });
  await Storage.saveArticleDraft(did, draft());
  const all = spyOn(Sync, "syncArticleDrafts").mockImplementation(value => Storage.listArticleDrafts(value.viewerDid)); restores.push(() => all.mockRestore());
  const one = spyOn(Sync, "syncArticleDraft").mockImplementation((value, key) => Storage.getArticleDraft(value.viewerDid, key)); restores.push(() => one.mockRestore());
});
afterEach(async () => { cleanup(); await new Promise(resolve => setTimeout(resolve, 0)); restores.splice(0).reverse().forEach(restore => restore()); if (originalIDB) Object.defineProperty(globalThis, "indexedDB", originalIDB); else Reflect.deleteProperty(globalThis, "indexedDB"); });
async function show() {
  const view = renderHook(() => useArticleDrafts(did, actor));
  await waitFor(() => expect(view.result.current.status).toBe("Synced Privately"));
  return view;
}
it("loads local recovery immediately and asks the private service to migrate/reconcile it", async () => {
  const gate = deferred<ArticleDraft[]>();
  const all = spyOn(Sync, "syncArticleDrafts").mockReturnValue(gate.promise); restores.push(() => all.mockRestore());
  const view = renderHook(() => useArticleDrafts(did, actor));
  await waitFor(() => expect(view.result.current.loading).toBe(false));
  expect(view.result.current.draft?.title).toBe("Legacy Private Draft");
  expect(all.mock.calls[0][0]).toMatchObject({ viewerDid: did, session });
  expect(view.result.current.syncing).toBe(true);
  await act(async () => { gate.resolve(await Storage.listArticleDrafts(did)); });
  await waitFor(() => expect(view.result.current.status).toBe("Synced Privately"));
});
it("does not queue durable local typing behind a slow remote save or replace it with its older response", async () => {
  const { result } = await show();
  const gate = deferred<ArticleDraft | undefined>();
  const one = spyOn(Sync, "syncArticleDraft").mockReturnValue(gate.promise); restores.push(() => one.mockRestore());
  act(() => result.current.update({ title: "First Edit" }));
  await act(async () => result.current.persist());
  const first = (await Storage.getArticleDraft(did, id))!;
  expect(first.title).toBe("First Edit");
  expect(result.current.syncing).toBe(true);
  act(() => result.current.update({ title: "Latest Typing" }));
  await act(async () => result.current.persist({ localOnly: true }));
  expect((await Storage.getArticleDraft(did, id))?.title).toBe("Latest Typing");
  await act(async () => { gate.resolve(first); });
  expect(result.current.draft?.title).toBe("Latest Typing");
});
it("requires a remote acknowledgement for publication while retaining locally saved edits on failure", async () => {
  const { result } = await show();
  const one = spyOn(Sync, "syncArticleDraft").mockRejectedValue(new Error("Private Service Offline")); restores.push(() => one.mockRestore());
  act(() => result.current.update({ title: "Keep This Article" }));
  await act(async () => { await expect(result.current.persist({ requireRemote: true })).rejects.toThrow("Private Service Offline"); });
  expect((await Storage.getArticleDraft(did, id))?.title).toBe("Keep This Article");
  expect(result.current.draft?.title).toBe("Keep This Article");
  expect(result.current.status).toBe("Saved Locally · Sync Pending");
  expect(result.current.error).toBe("Private Service Offline");
});
it("refreshes the private list after reconnect and aborts its actor when unmounted", async () => {
  const { result, unmount } = await show();
  const all = spyOn(Sync, "syncArticleDrafts").mockImplementation(value => Storage.listArticleDrafts(value.viewerDid)); restores.push(() => all.mockRestore());
  act(() => result.current.update({ title: "Offline Edit" }));
  await act(async () => result.current.persist({ localOnly: true }));
  fireEvent(window, new window.Event("online"));
  await waitFor(() => expect(all).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(result.current.syncing).toBe(false));
  const active = all.mock.calls[0][0];
  expect((await Storage.getArticleDraft(did, id))?.title).toBe("Offline Edit");
  unmount();
  expect(active.signal?.aborted).toBe(true);
});
it("fences a retired same-DID session response without installing stale content or error into its replacement", async () => {
  let generation = 1;
  const factory = (expected: number): (() => ArticleDraftActor) => () => ({ session: { did } as unknown as OAuthSession, viewerDid: did, assertCurrentActor() { if (generation !== expected) throw new Error("Retired Session"); } });
  const firstActor = factory(1), nextActor = factory(2);
  const { result, rerender } = renderHook(({ getActor }) => useArticleDrafts(did, getActor), { initialProps: { getActor: firstActor } });
  await waitFor(() => expect(result.current.status).toBe("Synced Privately"));
  const gate = deferred<ArticleDraft | undefined>();
  const one = spyOn(Sync, "syncArticleDraft").mockReturnValue(gate.promise); restores.push(() => one.mockRestore());
  act(() => result.current.update({ title: "Current Session Edits" }));
  await act(async () => result.current.persist());
  generation = 2; rerender({ getActor: nextActor });
  await waitFor(() => expect(result.current.status).toBe("Synced Privately"));
  await act(async () => { gate.resolve({ ...draft(), title: "Old Session Reply", revision: 1 }); });
  expect(result.current.draft?.title).toBe("Current Session Edits");
  expect(result.current.error).toBeUndefined();
  expect(result.current.status).toBe("Synced Privately");
});

it("never starts cloud writes if the local journal cannot persist the edited snapshot", async () => {
  const { result } = await show();
  const one = spyOn(Sync, "syncArticleDraft"); restores.push(() => one.mockRestore());
  const save = spyOn(Storage, "saveArticleDraft").mockRejectedValue(new Error("Local Storage Full")); restores.push(() => save.mockRestore());
  act(() => result.current.update({ title: "Unsaved Local Text" }));
  await act(async () => { await expect(result.current.persist()).rejects.toThrow("Local Storage Full"); });
  expect(one).not.toHaveBeenCalled();
  expect(result.current.draft?.title).toBe("Unsaved Local Text");
  expect(result.current.status).toBe("Not Saved");
});

it("does not let delayed locked-draft recovery replace a different draft selected meanwhile", async () => {
  const other = await Storage.saveArticleDraft(did, { ...draft(), id: "33333333-3333-4333-8333-333333333333", title: "Other Draft", updatedAt: "2026-10-01T00:00:00Z" });
  const { result } = await show();
  const remoteReply = deferred<ArticleDraft | undefined>();
  const recovery = deferred<Awaited<ReturnType<typeof Storage.updateArticleDraftSyncState>>>();
  const one = spyOn(Sync, "syncArticleDraft").mockReturnValueOnce(remoteReply.promise).mockImplementation((value, key) => Storage.getArticleDraft(value.viewerDid, key)); restores.push(() => one.mockRestore());
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockResolvedValue(other); restores.push(() => load.mockRestore());
  act(() => result.current.update({ title: "First Edit" }));
  await act(async () => result.current.persist());
  const saved = (await Storage.getArticleDraft(did, id))!;
  act(() => result.current.update({ title: "Unsaved While Claim Arrives" }));
  const checkpoint = spyOn(Storage, "updateArticleDraftSyncState").mockReturnValue(recovery.promise); restores.push(() => checkpoint.mockRestore());
  act(() => { remoteReply.resolve({ ...saved, publishingRecordKey: "claim", publicationClaimId: crypto.randomUUID() }); });
  await waitFor(() => expect(checkpoint).toHaveBeenCalledTimes(1));
  await act(async () => result.current.select(other.id));
  expect(result.current.draft?.id).toBe(other.id);
  await act(async () => { recovery.resolve({ draftId: id, serverRevision: 1, ackedLocalRevision: saved.revision! }); });
  expect(result.current.draft?.id).toBe(other.id);
});

it("does not install a selection response from a retired same-DID authorization generation", async () => {
  let generation = 1;
  const makeActor = (expected: number) => () => ({ session, viewerDid: did, assertCurrentActor() { if (generation !== expected) throw new Error("Retired Selection Session"); } });
  const firstActor = makeActor(1), nextActor = makeActor(2);
  const other = { ...draft(), id: "33333333-3333-4333-8333-333333333333", title: "Uncached Remote Draft", updatedAt: "2026-10-01T00:00:00Z" };
  const { result, rerender } = renderHook(({ getActor }) => useArticleDrafts(did, getActor), { initialProps: { getActor: firstActor } });
  await waitFor(() => expect(result.current.status).toBe("Synced Privately"));
  const gate = deferred<ArticleDraft | undefined>();
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockReturnValue(gate.promise); restores.push(() => load.mockRestore());
  let failure: unknown;
  const selecting = result.current.select(other.id).catch(error => { failure = error; });
  await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
  generation = 2; rerender({ getActor: nextActor });
  await waitFor(() => expect(result.current.status).toBe("Synced Privately"));
  await act(async () => { gate.resolve({ ...other, title: "Retired Selection Reply" }); await selecting; });
  expect(failure).toBeInstanceOf(Error);
  expect(result.current.draft?.id).toBe(id);
  expect(result.current.error).toBeUndefined();
});

it("does not downgrade the local revision when an older list refresh arrives after a newer local save", async () => {
  const { result } = await show();
  const previous = (await Storage.getArticleDraft(did, id))!;
  const gate = deferred<ArticleDraft[]>();
  const all = spyOn(Sync, "syncArticleDrafts").mockReturnValue(gate.promise); restores.push(() => all.mockRestore());
  const refreshing = result.current.refresh();
  await waitFor(() => expect(all).toHaveBeenCalledTimes(1));
  act(() => result.current.update({ title: "Newer Local Save" }));
  await act(async () => result.current.persist({ localOnly: true }));
  await act(async () => { gate.resolve([previous]); await refreshing; });
  expect(result.current.draft?.title).toBe("Newer Local Save");
  act(() => result.current.update({ title: "Next Local Save" }));
  await act(async () => result.current.persist({ localOnly: true }));
  expect((await Storage.getArticleDraft(did, id))?.title).toBe("Next Local Save");
});

it("opens a cached selection without waiting for stalled initial or per-draft cloud work", async () => {
  realSync();
  const cached = await Storage.saveArticleDraft(did, { ...draft(), id: "33333333-3333-4333-8333-333333333333", title: "Cached Selection", updatedAt: "2026-10-01T00:00:00Z" });
  const never = new Promise<never>(() => {});
  const all = spyOn(Cloud, "listCloudArticleDrafts").mockImplementation(context => abortable(context, never)); restores.push(() => all.mockRestore());
  const get = spyOn(Cloud, "getCloudArticleDraft").mockImplementation(context => abortable(context, never)); restores.push(() => get.mockRestore());
  const { result } = renderHook(() => useArticleDrafts(did, actor));
  await waitFor(() => expect(result.current.loading).toBe(false));
  let selected = false;
  act(() => { void result.current.select(cached.id).then(() => { selected = true; }); });
  await waitFor(() => expect(result.current.draft?.id).toBe(cached.id));
  expect(result.current.draft?.title).toBe("Cached Selection");
  expect(selected).toBe(true);
});

it("preserves edits made after opening a cached draft when its late cloud reply arrives", async () => {
  realSync();
  const cached = await Storage.saveArticleDraft(did, { ...draft(), id: "44444444-4444-4444-8444-444444444444", title: "Cached Selection", updatedAt: "2026-10-01T00:00:00Z" });
  const never = new Promise<never>(() => {}), reply = deferred<ArticleDraftDocument | undefined>();
  const all = spyOn(Cloud, "listCloudArticleDrafts").mockImplementation(context => abortable(context, never)); restores.push(() => all.mockRestore());
  const get = spyOn(Cloud, "getCloudArticleDraft").mockImplementation(context => abortable(context, reply.promise)); restores.push(() => get.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => ({ ...cloudReply(input.draft), revision: input.expectedRevision + 1, mutationId: input.mutationId })); restores.push(() => put.mockRestore());
  const { result } = renderHook(() => useArticleDrafts(did, actor));
  await waitFor(() => expect(result.current.loading).toBe(false));
  act(() => { void result.current.select(cached.id); });
  await waitFor(() => expect(result.current.draft?.id).toBe(cached.id));
  act(() => result.current.update({ title: "Typed Immediately After Opening" }));
  await act(async () => result.current.persist({ localOnly: true }));
  await act(async () => { reply.resolve(cloudReply({ ...cached, title: "Old Cloud Reply" })); });
  await waitFor(() => expect(put).toHaveBeenCalled());
  expect(result.current.draft?.title).toBe("Typed Immediately After Opening");
  expect((await Storage.getArticleDraft(did, cached.id))?.title).toBe("Typed Immediately After Opening");
});

it("preserves a dirty outgoing draft and starts one background sync for each affected cached draft", async () => {
  const other = await Storage.saveArticleDraft(did, { ...draft(), id: "33333333-3333-4333-8333-333333333333", title: "Other Cached Draft", updatedAt: "2026-10-01T00:00:00Z" });
  const { result } = await show();
  const never = new Promise<never>(() => {});
  const one = spyOn(Sync, "syncArticleDraft").mockReturnValue(never); restores.push(() => one.mockRestore());
  const load = spyOn(Sync, "loadSyncedArticleDraft").mockReturnValue(never); restores.push(() => load.mockRestore());
  act(() => result.current.update({ title: "Outgoing Private Edits" }));
  await act(async () => result.current.select(other.id));
  expect(result.current.draft?.id).toBe(other.id);
  expect((await Storage.getArticleDraft(did, id))?.title).toBe("Outgoing Private Edits");
  expect(one.mock.calls.map(call => call[1]).sort()).toEqual([id, other.id].sort());
  expect(load).not.toHaveBeenCalled();
});
