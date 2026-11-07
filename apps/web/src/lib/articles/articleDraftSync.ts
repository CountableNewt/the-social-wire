import { describeDraftAttachment, downloadDraftAttachment, draftAttachmentSHA256, isRetiredDraftAttachment, uploadDraftAttachment } from "@/lib/socialDraftAttachmentClient";
import { assertArticleDraftActor, ArticleDraftRequestError, getCloudArticleDraft, listCloudArticleDrafts, putCloudArticleDraft, deleteCloudArticleDraft, validateArticleDraftPayload } from "./articleDraftCloudClient";
import type { ArticleDraftActor, ArticleDraftDocument, ArticleDraftPending, ArticleDraftPayload, ArticleDraftSyncState } from "./articleDraftCloudTypes";
import type { ArticleDraft } from "./articleDraftTypes";
import { applyRemoteArticleDraftDeletion, cacheRemoteArticleDraft, getArticleDraft, getArticleDraftAsset, getArticleDraftSyncState, listArticleDrafts, listArticleDraftSyncStates, tombstoneArticleDraft, updateArticleDraftSyncState } from "./articleDraftStorage";

export type { ArticleDraftActor } from "./articleDraftCloudTypes";
export { getArticleDraftSyncState } from "./articleDraftStorage";
export class ArticleDraftConflictError extends Error {
  constructor(public readonly remote: ArticleDraftDocument | undefined, public readonly local?: ArticleDraft) { super("The Article Draft Changed While Syncing. Your Local Copy Is Safe. Retry."); }
}
const queues = new Map<string, Promise<unknown>>();
function serialized<T>(actor: ArticleDraftActor, draftId: string, work: () => Promise<T>): Promise<T> {
  const key = `${actor.viewerDid}:${draftId}`, previous = queues.get(key) ?? Promise.resolve();
  const pending = previous.catch(() => {}).then(() => { assertArticleDraftActor(actor); return work(); });
  queues.set(key, pending);
  void pending.finally(() => { if (queues.get(key) === pending) queues.delete(key); }).catch(() => {});
  return pending;
}
function payload(draft: ArticleDraft): ArticleDraftPayload {
  const { revision: _revision, ...value } = structuredClone(draft); void _revision;
  return validateArticleDraftPayload({ ...value, assets: value.assets ?? [] });
}
function baseState(draftId: string): ArticleDraftSyncState { return { draftId, serverRevision: 0, ackedLocalRevision: 0 }; }
function locked(draft: ArticleDraft) { return !!(draft.publishedUri || draft.scheduleRecordKey || draft.publishingRecordKey); }
function remoteWins(remote: ArticleDraftDocument, local?: ArticleDraft): boolean {
  if (remote.deleted || !local || !remote.draft) return true;
  if (locked(remote.draft)) return true;
  const remoteTime = Date.parse(remote.draft.updatedAt), localTime = Date.parse(local.updatedAt);
  return !Number.isFinite(remoteTime) || !Number.isFinite(localTime) || remoteTime >= localTime;
}
async function adopt(actor: ArticleDraftActor, remote: ArticleDraftDocument): Promise<ArticleDraft | undefined> {
  assertArticleDraftActor(actor);
  let local = await getArticleDraft(actor.viewerDid, remote.draftId), state = await getArticleDraftSyncState(actor.viewerDid, remote.draftId);
  assertArticleDraftActor(actor);
  if ((state?.serverRevision ?? 0) > remote.revision) return local;
  if (remote.deleted) {
    await applyRemoteArticleDraftDeletion(actor.viewerDid, { ...state, ...baseState(remote.draftId), serverRevision: remote.revision, deleted: true, assets: [], pending: undefined });
    assertArticleDraftActor(actor); return undefined;
  }
  if (state?.deleted) return undefined;
  const adoptingMutation = state?.pending?.request.mutationId;
  const blobs: { assetId: string; blob: Blob }[] = [];
  for (const binding of remote.attachments) {
    assertArticleDraftActor(actor);
    let blob = await getArticleDraftAsset(actor.viewerDid, remote.draftId, binding.localAssetId);
    if (!blob || blob.type !== binding.asset.contentType || blob.size !== binding.asset.byteLength || await draftAttachmentSHA256(blob) !== binding.asset.sha256) blob = await downloadDraftAttachment(actor, binding.asset);
    assertArticleDraftActor(actor);
    blobs.push({ assetId: binding.localAssetId, blob });
  }
  // Image downloads may take long enough for another local edit or deletion.
  // Decide against the durable snapshot immediately before replacing the cache.
  local = await getArticleDraft(actor.viewerDid, remote.draftId);
  state = await getArticleDraftSyncState(actor.viewerDid, remote.draftId);
  assertArticleDraftActor(actor);
  if (state?.deleted) return undefined;
  if ((state?.serverRevision ?? 0) > remote.revision) return local;
  if (state?.pending?.request.mutationId !== adoptingMutation) return reconcile(actor, remote.draftId);
  if (local && local.revision !== state?.ackedLocalRevision && !remoteWins(remote, local)) {
    await updateArticleDraftSyncState(actor.viewerDid, remote.draftId, previous => {
      if (previous?.deleted || (previous?.serverRevision ?? 0) > remote.revision) return previous ?? baseState(remote.draftId);
      if (previous?.pending?.request.mutationId !== state?.pending?.request.mutationId) return previous ?? baseState(remote.draftId);
      return { ...(previous ?? baseState(remote.draftId)), serverRevision: remote.revision, assets: remote.attachments, pending: undefined };
    });
    return reconcile(actor, remote.draftId);
  }
  const dirty = !!local && local.revision !== state?.ackedLocalRevision;
  const next: ArticleDraftSyncState = { ...state, draftId: remote.draftId, serverRevision: remote.revision, ackedLocalRevision: 0, assets: remote.attachments, pending: undefined, conflict: undefined, recoveryDraft: dirty ? local : state?.recoveryDraft };
  const saved = await cacheRemoteArticleDraft(actor.viewerDid, remote.draft!, local?.revision, next, blobs, state?.pending?.request.mutationId);
  assertArticleDraftActor(actor); return saved;
}
async function prepare(actor: ArticleDraftActor, draft: ArticleDraft, state: ArticleDraftSyncState): Promise<ArticleDraftSyncState> {
  const snapshot = payload(draft), assets: Extract<ArticleDraftPending, { kind: "put" }>["assets"] = [];
  for (const descriptor of snapshot.assets) {
    const blob = await getArticleDraftAsset(actor.viewerDid, draft.id, descriptor.id);
    assertArticleDraftActor(actor);
    if (!blob) throw new Error(`The Private Image “${descriptor.name}” Is Missing. Restore It Before Syncing. Your Local Copy Is Safe.`);
    const previous = state.assets?.find(value => value.localAssetId === descriptor.id)?.asset;
    const asset = await describeDraftAttachment(blob, descriptor.name, previous);
    assertArticleDraftActor(actor);
    assets.push({ localAssetId: descriptor.id, asset });
  }
  const pending: ArticleDraftPending = { kind: "put", localRevision: draft.revision ?? 0, assets, request: { expectedRevision: state.serverRevision, mutationId: crypto.randomUUID(), draft: snapshot, attachments: assets.map(value => ({ localAssetId: value.localAssetId, assetId: value.asset.assetId })) } };
  assertArticleDraftActor(actor);
  return updateArticleDraftSyncState(actor.viewerDid, draft.id, previous => {
    if (previous?.deleted || previous?.pending) return previous;
    if ((previous?.serverRevision ?? 0) !== state.serverRevision) throw new Error("Article Draft Sync Advanced. Retry.");
    return { ...state, pending };
  });
}
async function reconcile(actor: ArticleDraftActor, draftId: string): Promise<ArticleDraft | undefined> {
  attempts: for (let attempt = 0; attempt < 5; attempt++) {
    assertArticleDraftActor(actor);
    let state = await getArticleDraftSyncState(actor.viewerDid, draftId) ?? baseState(draftId);
    let local = await getArticleDraft(actor.viewerDid, draftId);
    assertArticleDraftActor(actor);
    if (!state.pending) {
      if (state.deleted) return undefined;
      const remote = await getCloudArticleDraft(actor, draftId);
      // Cloud reads must not freeze the local winner decision at request time.
      // Another tab or the editor can save while this request is in flight.
      state = await getArticleDraftSyncState(actor.viewerDid, draftId) ?? baseState(draftId);
      local = await getArticleDraft(actor.viewerDid, draftId);
      assertArticleDraftActor(actor);
      if (state.pending || state.deleted || remote && remote.revision < state.serverRevision) continue attempts;
      if (remote) {
        if (remote.deleted || remote.revision !== state.serverRevision) {
          if (remoteWins(remote, local) || local?.revision === state.ackedLocalRevision) return adopt(actor, remote);
          state = await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => {
            if (previous?.pending || previous?.deleted) return previous;
            return { ...(previous ?? state), serverRevision: remote.revision, assets: remote.attachments };
          });
        }
      } else if (state.serverRevision) throw new Error("The Private Article Draft Is Unavailable. Your Local Copy Is Safe.");
      local = await getArticleDraft(actor.viewerDid, draftId);
      assertArticleDraftActor(actor);
      if (!local) return undefined;
      if (state.serverRevision && local.revision === state.ackedLocalRevision) return local;
      state = await prepare(actor, local, state);
    }
    const pending = state.pending;
    if (!pending) continue;
    try {
      let response: ArticleDraftDocument;
      if (pending.kind === "delete") response = await deleteCloudArticleDraft(actor, pending.request);
      else {
        // A resumed image mutation may already be committed, or another device
        // may have deleted it and retired its files. Observe that before uploads.
        const remote = pending.assets.length ? await getCloudArticleDraft(actor, draftId) : undefined;
        if (remote?.deleted) return adopt(actor, remote);
        if (remote?.draft && locked(remote.draft) && remote.mutationId !== pending.request.mutationId) {
          const latest = await getArticleDraftSyncState(actor.viewerDid, draftId);
          if (latest?.deleteRequested) {
            await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: remote.revision, pending: { kind: "delete", request: { draftId, expectedRevision: remote.revision, mutationId: crypto.randomUUID() } } } : previous ?? state);
            continue attempts;
          }
          return adopt(actor, remote);
        }
        if (remote?.mutationId === pending.request.mutationId) response = remote;
        else {
          // IDs and the complete payload are durable before even the first upload.
          let confirmed: ArticleDraftDocument | undefined;
          for (const binding of pending.assets) {
            assertArticleDraftActor(actor);
            const blob = await getArticleDraftAsset(actor.viewerDid, draftId, binding.localAssetId);
            if (!blob) throw new Error("A Private Article Image Is Missing. Your Local Copy Is Safe.");
            try { await uploadDraftAttachment(actor, binding.asset, blob); }
            catch (failure) {
              assertArticleDraftActor(actor);
              if (!isRetiredDraftAttachment(failure)) throw failure;
              const currentRemote = await getCloudArticleDraft(actor, draftId);
              assertArticleDraftActor(actor);
              if (currentRemote?.mutationId === pending.request.mutationId) { confirmed = currentRemote; break; }
              if (currentRemote && remoteWins(currentRemote, pending.request.draft)) {
                const latest = await getArticleDraftSyncState(actor.viewerDid, draftId);
                if (latest?.deleteRequested && !currentRemote.deleted) {
                  await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: currentRemote.revision, pending: { kind: "delete", request: { draftId, expectedRevision: currentRemote.revision, mutationId: crypto.randomUUID() } } } : previous ?? state);
                  continue attempts;
                }
                return adopt(actor, currentRemote);
              }
              // AssetRetired proves this private identity cannot be reused. First
              // reconcile the article, then durably rotate only the expired file.
              const assets = pending.assets.map(value => value.localAssetId === binding.localAssetId ? { ...value, asset: { ...value.asset, assetId: crypto.randomUUID(), state: "reserved" as const } } : value);
              const request = { ...pending.request, expectedRevision: currentRemote?.revision ?? 0, mutationId: crypto.randomUUID(), attachments: assets.map(value => ({ localAssetId: value.localAssetId, assetId: value.asset.assetId })) };
              await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: request.expectedRevision, pending: { ...pending, assets, request } } : previous ?? state);
              assertArticleDraftActor(actor);
              continue attempts;
            }
            assertArticleDraftActor(actor);
          }
          response = confirmed ?? await putCloudArticleDraft(actor, pending.request);
        }
      }
      assertArticleDraftActor(actor);
      await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => {
        if (previous?.pending?.request.mutationId !== pending.request.mutationId) return previous ?? state;
        return { ...previous, serverRevision: response.revision, ackedLocalRevision: pending.kind === "put" ? pending.localRevision : previous.ackedLocalRevision, pending: previous.deleted && !response.deleted ? { kind: "delete", request: { draftId, expectedRevision: response.revision, mutationId: crypto.randomUUID() } } : undefined, deleted: previous.deleted || response.deleted, assets: response.attachments };
      });
      assertArticleDraftActor(actor);
      if (response.deleted) { await applyRemoteArticleDraftDeletion(actor.viewerDid, { draftId, serverRevision: response.revision, ackedLocalRevision: 0, deleted: true }); return undefined; }
      local = await getArticleDraft(actor.viewerDid, draftId);
      if (pending.kind === "put" && local?.revision === pending.localRevision) return local;
    } catch (error) {
      assertArticleDraftActor(actor);
      if (!(error instanceof ArticleDraftRequestError) || error.status !== 409) throw error;
      const remote = await getCloudArticleDraft(actor, draftId);
      if (!remote) throw new ArticleDraftConflictError(remote, local);
      local = await getArticleDraft(actor.viewerDid, draftId);
      if (pending.kind === "delete") {
        if (remote.deleted) return adopt(actor, remote);
        await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: remote.revision, pending: { kind: "delete", request: { ...pending.request, expectedRevision: remote.revision, mutationId: crypto.randomUUID() } } } : previous ?? state);
      } else if ((await getArticleDraftSyncState(actor.viewerDid, draftId))?.deleted && !remote.deleted) {
        await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: remote.revision, pending: { kind: "delete", request: { draftId, expectedRevision: remote.revision, mutationId: crypto.randomUUID() } } } : previous ?? state);
      } else if (remoteWins(remote, local)) return adopt(actor, remote);
      else await updateArticleDraftSyncState(actor.viewerDid, draftId, previous => previous?.pending?.request.mutationId === pending.request.mutationId ? { ...previous, serverRevision: remote.revision, pending: undefined, assets: remote.attachments } : previous ?? state);
    }
  }
  throw new Error("Article Drafts Are Changing on Another Device. Your Local Copy Is Safe. Retry Sync.");
}
export function syncArticleDraft(actor: ArticleDraftActor, draftId: string): Promise<ArticleDraft | undefined> {
  return serialized(actor, draftId, () => reconcile(actor, draftId));
}
export async function loadSyncedArticleDraft(actor: ArticleDraftActor, draftId: string): Promise<ArticleDraft | undefined> {
  try { return await syncArticleDraft(actor, draftId); }
  catch (error) {
    assertArticleDraftActor(actor);
    const state = await getArticleDraftSyncState(actor.viewerDid, draftId), local = await getArticleDraft(actor.viewerDid, draftId);
    if (local && !state?.deleted && !(error instanceof ArticleDraftRequestError && [401, 403].includes(error.status))) return local;
    throw error;
  }
}
export async function removeSyncedArticleDraft(actor: ArticleDraftActor, draftId: string): Promise<void> {
  await serialized(actor, draftId, async () => {
    assertArticleDraftActor(actor);
    // The tombstone is durable even if a previous put response was lost. Its
    // identity is reconciled before a separate revisioned deletion is sent.
    await tombstoneArticleDraft(actor.viewerDid, draftId, crypto.randomUUID());
    await reconcile(actor, draftId);
  });
}
export async function syncArticleDrafts(actor: ArticleDraftActor): Promise<ArticleDraft[]> {
  assertArticleDraftActor(actor);
  const remote = await listCloudArticleDrafts(actor);
  for (const document of remote) {
    await serialized(actor, document.draftId, async () => {
      const local = await getArticleDraft(actor.viewerDid, document.draftId), state = await getArticleDraftSyncState(actor.viewerDid, document.draftId);
      assertArticleDraftActor(actor);
      if (state?.deleted && !document.deleted || state?.pending) { await reconcile(actor, document.draftId); return; }
      if (document.deleted || !local || document.revision !== state?.serverRevision) {
        if (remoteWins(document, local) || local?.revision === state?.ackedLocalRevision) await adopt(actor, document);
        else await reconcile(actor, document.draftId);
      }
    });
  }
  const drafts = await listArticleDrafts(actor.viewerDid);
  for (const draft of drafts) {
    const state = await getArticleDraftSyncState(actor.viewerDid, draft.id);
    if (!state?.serverRevision || state.pending || draft.revision !== state.ackedLocalRevision) await syncArticleDraft(actor, draft.id);
  }
  for (const state of await listArticleDraftSyncStates(actor.viewerDid)) if (state.pending) await syncArticleDraft(actor, state.draftId);
  assertArticleDraftActor(actor);
  return listArticleDrafts(actor.viewerDid);
}
