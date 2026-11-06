import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { IDBFactory } from "fake-indexeddb";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Cloud from "@/lib/articles/articleDraftCloudClient";
import * as Files from "@/lib/socialDraftAttachmentClient";
import * as Storage from "@/lib/articles/articleDraftStorage";
import { loadSyncedArticleDraft, removeSyncedArticleDraft, syncArticleDraft, syncArticleDrafts } from "@/lib/articles/articleDraftSync";
import type { ArticleDraftActor, ArticleDraftDocument, ArticleDraftPut } from "@/lib/articles/articleDraftCloudTypes";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
const did = "did:plc:alice", id = "11111111-1111-4111-8111-111111111111", assetId = "22222222-2222-4222-8222-222222222222";
const actor: ArticleDraftActor = { viewerDid: did, session: { did } as unknown as OAuthSession };
const restores: (() => void)[] = [];
const originalIDB = Object.getOwnPropertyDescriptor(globalThis, "indexedDB");
let remote: ArticleDraftDocument | undefined;
function draft(title = "Local Article", updatedAt = "2026-11-06T00:00:00Z"): ArticleDraft { return { id, title, markdown: "Private body", excerpt: "", path: "", tags: [], publicationUri: "", createdAt: "2026-11-01T00:00:00Z", updatedAt, assets: [] }; }
function document(value = draft(), revision = 1): ArticleDraftDocument { const { revision: _revision, ...payload } = value; void _revision; return { draftId: value.id, revision, deleted: false, mutationId: crypto.randomUUID(), updatedAt: value.updatedAt, draft: { ...payload, assets: payload.assets ?? [] }, attachments: [] }; }
beforeEach(() => {
  Object.defineProperty(globalThis, "indexedDB", { configurable: true, value: new IDBFactory() }); remote = undefined;
  const get = spyOn(Cloud, "getCloudArticleDraft").mockImplementation(async () => remote); restores.push(() => get.mockRestore());
  const list = spyOn(Cloud, "listCloudArticleDrafts").mockImplementation(async () => remote ? [remote] : []); restores.push(() => list.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    remote = { draftId: input.draft.id, revision: input.expectedRevision + 1, deleted: false, mutationId: input.mutationId, updatedAt: input.draft.updatedAt, draft: input.draft, attachments: [] }; return remote;
  }); restores.push(() => put.mockRestore());
  const remove = spyOn(Cloud, "deleteCloudArticleDraft").mockImplementation(async (_actor, input) => { remote = { draftId: input.draftId, revision: input.expectedRevision + 1, deleted: true, mutationId: input.mutationId, updatedAt: "2026-11-07T00:00:00Z", attachments: [] }; return remote; }); restores.push(() => remove.mockRestore());
});
afterEach(() => { restores.splice(0).reverse().forEach(restore => restore()); if (originalIDB) Object.defineProperty(globalThis, "indexedDB", originalIDB); else Reflect.deleteProperty(globalThis, "indexedDB"); });
it("keeps an offline local draft intact and never treats it as remotely acknowledged", async () => {
  await Storage.saveArticleDraft(did, draft());
  const get = spyOn(Cloud, "getCloudArticleDraft").mockRejectedValue(new Error("Offline")); restores.push(() => get.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Offline");
  expect((await loadSyncedArticleDraft(actor, id))?.title).toBe("Local Article");
  expect(await Storage.getArticleDraftSyncState(did, id)).toBeUndefined();
  expect(await Storage.listArticleDrafts(did)).toHaveLength(1);
});
it("persists an immutable mutation before sending and retries the same lost-response mutation", async () => {
  await Storage.saveArticleDraft(did, draft());
  const requests: ArticleDraftPut[] = [];
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    requests.push(structuredClone(input));
    expect((await Storage.getArticleDraftSyncState(did, id))?.pending?.request.mutationId).toBe(input.mutationId);
    remote ??= { ...document(input.draft), mutationId: input.mutationId };
    if (requests.length === 1) throw new Error("Response Lost");
    return remote;
  }); restores.push(() => put.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Response Lost");
  expect((await Storage.getArticleDraftSyncState(did, id))?.pending).toBeDefined();
  expect((await syncArticleDraft(actor, id))?.title).toBe("Local Article");
  expect(requests[1]).toEqual(requests[0]);
  expect((await Storage.getArticleDraftSyncState(did, id))).toMatchObject({ serverRevision: 1, ackedLocalRevision: 1, pending: undefined });
});
it("adopts a newer server edit while retaining the superseded local recovery payload", async () => {
  await Storage.saveArticleDraft(did, draft("Old Local", "2026-11-02T00:00:00Z"));
  remote = document(draft("Newest Remote", "2026-11-07T00:00:00Z"), 3);
  expect((await syncArticleDraft(actor, id))?.title).toBe("Newest Remote");
  expect((await Storage.getArticleDraftSyncState(did, id))?.recoveryDraft?.title).toBe("Old Local");
});
it("rebases the newer local edit after CAS conflict with a distinct durable mutation ID", async () => {
  await Storage.saveArticleDraft(did, draft("Newest Local", "2026-11-09T00:00:00Z"));
  remote = document(draft("Old Remote", "2026-11-02T00:00:00Z"), 2);
  const inputs: ArticleDraftPut[] = [];
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    inputs.push(structuredClone(input));
    if (inputs.length === 1) { remote = document(draft("Other Edit", "2026-11-03T00:00:00Z"), 4); throw new Cloud.ArticleDraftRequestError("Conflict", 409); }
    return remote = { ...document(input.draft, input.expectedRevision + 1), mutationId: input.mutationId };
  }); restores.push(() => put.mockRestore());
  expect((await syncArticleDraft(actor, id))?.title).toBe("Newest Local");
  expect(inputs.map(value => value.expectedRevision)).toEqual([2, 4]);
  expect(inputs[1].mutationId).not.toBe(inputs[0].mutationId);
  expect(inputs[1].draft.title).toBe("Newest Local");
});
it("lets a remote publication claim win even when the local text timestamp is newer", async () => {
  await Storage.saveArticleDraft(did, draft("Newer Text", "2026-11-09T00:00:00Z"));
  remote = document({ ...draft("Claimed Snapshot", "2026-11-02T00:00:00Z"), publishingRecordKey: "stable-key", publicationClaimId: crypto.randomUUID() }, 2);
  const saved = await syncArticleDraft(actor, id);
  expect(saved?.title).toBe("Claimed Snapshot"); expect(saved?.publicationClaimId).toEqual(remote.draft?.publicationClaimId);
});
it("treats server tombstones as authoritative and prevents stale local resurrection", async () => {
  const saved = await Storage.saveArticleDraft(did, draft());
  remote = { draftId: id, revision: 2, deleted: true, mutationId: crypto.randomUUID(), updatedAt: "2026-11-08T00:00:00Z", attachments: [] };
  expect(await syncArticleDraft(actor, id)).toBeUndefined();
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
  expect((await Storage.getArticleDraftSyncState(did, id))?.recoveryDraft?.markdown).toBe("Private body");
  await expect(Storage.saveArticleDraft(did, { ...saved, revision: undefined })).rejects.toThrow("deleted");
});
it("hydrates verified private image bytes on a fresh device without a PDS upload", async () => {
  const blob = new Blob(["private image"], { type: "image/png" });
  const asset = { assetId, name: "cover.png", contentType: "image/png", byteLength: blob.size, sha256: await Files.draftAttachmentSHA256(blob), kind: "photo" as const, state: "ready" as const };
  remote = document({ ...draft(), coverAssetId: assetId, assets: [{ id: assetId, alt: "Cover", name: asset.name, mimeType: blob.type, width: 10, height: 10 }] }); remote.attachments = [{ localAssetId: assetId, asset }];
  const download = spyOn(Files, "downloadDraftAttachment").mockResolvedValue(blob); restores.push(() => download.mockRestore());
  const result = await loadSyncedArticleDraft(actor, id);
  expect(result?.coverAssetId).toBe(assetId);
  expect(download).toHaveBeenCalledWith(actor, asset);
  expect(await (await Storage.getArticleDraftAsset(did, id, assetId))?.text()).toBe("private image");
});
it("retains a local deletion journal while offline and completes the same mutation after reconnect", async () => {
  await Storage.saveArticleDraft(did, draft()); await syncArticleDraft(actor, id);
  const remove = spyOn(Cloud, "deleteCloudArticleDraft").mockRejectedValueOnce(new Error("Offline")); restores.push(() => remove.mockRestore());
  await expect(removeSyncedArticleDraft(actor, id)).rejects.toThrow("Offline");
  const pending = (await Storage.getArticleDraftSyncState(did, id))?.pending;
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
  expect(pending?.kind).toBe("delete");
  remove.mockImplementation(async (_actor, input) => ({ draftId: id, revision: input.expectedRevision + 1, deleted: true, mutationId: input.mutationId, updatedAt: "2026-11-07T00:00:00Z", attachments: [] }));
  await syncArticleDrafts(actor);
  expect((await Storage.getArticleDraftSyncState(did, id))?.pending).toBeUndefined();
  expect(remove.mock.calls[1][1].mutationId).toBe(pending!.request.mutationId);
});
it("fences late account responses before hydrating another viewer's data", async () => {
  let active = true;
  const guarded: ArticleDraftActor = { ...actor, assertCurrentActor() { if (!active) throw new Error("Account Changed"); } };
  const get = spyOn(Cloud, "getCloudArticleDraft").mockImplementation(async () => { active = false; return document(); }); restores.push(() => get.mockRestore());
  await expect(loadSyncedArticleDraft(guarded, id)).rejects.toThrow("Account Changed");
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
  expect(await Storage.listArticleDrafts("did:plc:bob")).toEqual([]);
});

it("journals immutable private image IDs before upload and reuses them after a failed upload", async () => {
  const blob = new Blob(["private cover"], { type: "image/png" });
  await Storage.saveArticleDraft(did, { ...draft(), coverAssetId: assetId, assets: [{ id: assetId, alt: "Cover", name: "cover.png", mimeType: blob.type, width: 10, height: 10 }] });
  await Storage.saveArticleDraftAsset(did, id, assetId, blob);
  const attemptedIds: string[] = [];
  const upload = spyOn(Files, "uploadDraftAttachment").mockImplementation(async (_actor, asset) => {
    attemptedIds.push(asset.assetId);
    const pending = (await Storage.getArticleDraftSyncState(did, id))?.pending;
    expect(pending?.kind === "put" ? pending.assets[0].asset.assetId : undefined).toBe(asset.assetId);
    if (attemptedIds.length === 1) throw new Error("Upload Offline");
    return { ...asset, state: "ready" };
  }); restores.push(() => upload.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    const pending = (await Storage.getArticleDraftSyncState(did, id))!.pending!;
    return { ...document(input.draft), mutationId: input.mutationId, attachments: pending.kind === "put" ? pending.assets.map(binding => ({ ...binding, asset: { ...binding.asset, state: "ready" } })) : [] };
  }); restores.push(() => put.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Upload Offline");
  const mutation = (await Storage.getArticleDraftSyncState(did, id))!.pending!.request.mutationId;
  expect((await syncArticleDraft(actor, id))?.coverAssetId).toBe(assetId);
  expect(attemptedIds[1]).toBe(attemptedIds[0]);
  expect(put.mock.calls[0][1].mutationId).toBe(mutation);
  expect(await (await Storage.getArticleDraftAsset(did, id, assetId))?.text()).toBe("private cover");
});

it("durably hides a delete request even while an earlier put acknowledgement is uncertain", async () => {
  await Storage.saveArticleDraft(did, draft());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockRejectedValue(new Error("Offline")); restores.push(() => put.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Offline");
  const pendingId = (await Storage.getArticleDraftSyncState(did, id))!.pending!.request.mutationId;
  await expect(removeSyncedArticleDraft(actor, id)).rejects.toThrow("Offline");
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
  expect((await Storage.getArticleDraftSyncState(did, id))).toMatchObject({ deleted: true, pending: { kind: "put", request: { mutationId: pendingId } } });
  put.mockImplementation(async (_actor, input) => remote = { ...document(input.draft), mutationId: input.mutationId });
  await syncArticleDrafts(actor);
  expect((await Storage.getArticleDraftSyncState(did, id))).toMatchObject({ deleted: true, serverRevision: 2 });
  expect((await Storage.getArticleDraftSyncState(did, id))?.pending).toBeUndefined();
});

it("keeps late unsaved editor text in durable recovery without resurrecting a server-deleted draft", async () => {
  const saved = await Storage.saveArticleDraft(did, draft()); await syncArticleDraft(actor, id);
  remote = { draftId: id, revision: 2, deleted: true, mutationId: crypto.randomUUID(), updatedAt: "2026-11-08T00:00:00Z", attachments: [] };
  await syncArticleDraft(actor, id);
  await expect(Storage.saveArticleDraft(did, { ...saved, markdown: "Typed While Delete Arrived" })).rejects.toThrow("deleted");
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
  expect((await Storage.getArticleDraftSyncState(did, id))?.recoveryDraft?.markdown).toBe("Typed While Delete Arrived");
});

it("removes old recovery content and private files after an explicitly requested deletion is acknowledged", async () => {
  const saved = await Storage.saveArticleDraft(did, draft()); await syncArticleDraft(actor, id);
  await Storage.saveArticleDraftAsset(did, id, assetId, new Blob(["private bytes"]));
  await Storage.updateArticleDraftSyncState(did, id, state => ({ ...state!, recoveryDraft: saved }));
  await removeSyncedArticleDraft(actor, id);
  expect((await Storage.getArticleDraftSyncState(did, id))?.recoveryDraft).toBeUndefined();
  expect(await Storage.getArticleDraftAsset(did, id, assetId)).toBeUndefined();
});

it("observes a remote tombstone before retrying files from an uncertain image mutation", async () => {
  const blob = new Blob(["cover"], { type: "image/png" });
  await Storage.saveArticleDraft(did, { ...draft(), assets: [{ id: assetId, alt: "", name: "cover.png", mimeType: blob.type, width: 2, height: 2 }] });
  await Storage.saveArticleDraftAsset(did, id, assetId, blob);
  const upload = spyOn(Files, "uploadDraftAttachment").mockRejectedValue(new Error("Upload Offline")); restores.push(() => upload.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Upload Offline");
  remote = { draftId: id, revision: 2, deleted: true, mutationId: crypto.randomUUID(), updatedAt: "2026-11-07T00:00:00Z", attachments: [] };
  expect(await syncArticleDraft(actor, id)).toBeUndefined();
  expect(upload).toHaveBeenCalledTimes(1);
  expect(await Storage.listArticleDrafts(did)).toEqual([]);
});

it("rotates a definitively expired uncommitted private asset only after reconciling the article", async () => {
  const blob = new Blob(["private cover"], { type: "image/png" });
  await Storage.saveArticleDraft(did, { ...draft(), assets: [{ id: assetId, alt: "Cover", name: "cover.png", mimeType: blob.type, width: 10, height: 10 }] });
  await Storage.saveArticleDraftAsset(did, id, assetId, blob);
  const uploads: { asset: string; mutation: string; local: string }[] = [];
  const upload = spyOn(Files, "uploadDraftAttachment").mockImplementation(async (_actor, asset) => {
    const pending = (await Storage.getArticleDraftSyncState(did, id))!.pending!;
    if (pending.kind !== "put") throw new Error("Expected put");
    uploads.push({ asset: asset.assetId, mutation: pending.request.mutationId, local: pending.assets[0].localAssetId });
    expect(pending.request.attachments[0].assetId).toBe(asset.assetId);
    if (uploads.length === 1) throw Object.assign(new Error("Expired"), { status: 409, error: "AssetRetired" });
    return { ...asset, state: "ready" };
  }); restores.push(() => upload.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    const pending = (await Storage.getArticleDraftSyncState(did, id))!.pending!;
    return remote = { ...document(input.draft), mutationId: input.mutationId, attachments: pending.kind === "put" ? pending.assets.map(value => ({ ...value, asset: { ...value.asset, state: "ready" } })) : [] };
  }); restores.push(() => put.mockRestore());
  expect((await syncArticleDraft(actor, id))?.title).toBe("Local Article");
  expect(uploads).toHaveLength(2);
  expect(uploads[1].asset).not.toBe(uploads[0].asset);
  expect(uploads[1].mutation).not.toBe(uploads[0].mutation);
  expect(uploads.map(value => value.local)).toEqual([assetId, assetId]);
  expect(put).toHaveBeenCalledTimes(1);
  expect(await (await Storage.getArticleDraftAsset(did, id, assetId))?.text()).toBe("private cover");
});

it("acknowledges an already committed image draft before retrying an expired staged upload", async () => {
  const blob = new Blob(["cover"], { type: "image/png" });
  await Storage.saveArticleDraft(did, { ...draft(), assets: [{ id: assetId, alt: "", name: "cover.png", mimeType: blob.type, width: 2, height: 2 }] });
  await Storage.saveArticleDraftAsset(did, id, assetId, blob);
  const upload = spyOn(Files, "uploadDraftAttachment").mockImplementation(async (_actor, asset) => ({ ...asset, state: "ready" })); restores.push(() => upload.mockRestore());
  const put = spyOn(Cloud, "putCloudArticleDraft").mockImplementation(async (_actor, input) => {
    const pending = (await Storage.getArticleDraftSyncState(did, id))!.pending!;
    remote = { ...document(input.draft), mutationId: input.mutationId, attachments: pending.kind === "put" ? pending.assets.map(value => ({ ...value, asset: { ...value.asset, state: "ready" } })) : [] };
    throw new Error("Lost Commit Reply");
  }); restores.push(() => put.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Lost Commit Reply");
  const committedMutation = remote!.mutationId;
  upload.mockRejectedValue(Object.assign(new Error("Expired"), { status: 409, error: "AssetRetired" }));
  expect((await syncArticleDraft(actor, id))?.title).toBe("Local Article");
  expect(upload).toHaveBeenCalledTimes(1);
  expect(put).toHaveBeenCalledTimes(1);
  expect((await Storage.getArticleDraftSyncState(did, id))).toMatchObject({ serverRevision: 1, pending: undefined });
  expect(remote!.mutationId).toBe(committedMutation);
});

it("completes an explicit deletion when a pending image mutation encounters a remote lifecycle lock", async () => {
  const blob = new Blob(["cover"], { type: "image/png" });
  await Storage.saveArticleDraft(did, { ...draft(), assets: [{ id: assetId, alt: "", name: "cover.png", mimeType: blob.type, width: 2, height: 2 }] });
  await Storage.saveArticleDraftAsset(did, id, assetId, blob);
  const upload = spyOn(Files, "uploadDraftAttachment").mockRejectedValue(new Error("Upload Offline")); restores.push(() => upload.mockRestore());
  await expect(syncArticleDraft(actor, id)).rejects.toThrow("Upload Offline");
  remote = document({ ...draft(), publishedUri: "at://did:plc:alice/site.standard.document/published" }, 2);
  await removeSyncedArticleDraft(actor, id);
  expect((await Storage.getArticleDraftSyncState(did, id))).toMatchObject({ deleted: true, serverRevision: 3 });
  expect((await Storage.getArticleDraftSyncState(did, id))?.pending).toBeUndefined();
  expect(upload).toHaveBeenCalledTimes(1);
});
