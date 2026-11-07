import { gatewayFetch } from "@/lib/socialWireGatewayClient";
import { awaitWithAbort } from "@/lib/awaitWithAbort";
import type { ArticleDraftActor, ArticleDraftDelete, ArticleDraftDocument, ArticleDraftPayload, ArticleDraftPut } from "./articleDraftCloudTypes";
export type { ArticleDraftActor, ArticleDraftDocument, ArticleDraftPayload } from "./articleDraftCloudTypes";

const prefix = "/xrpc/app.thesocialwire.articleDraft.";
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function assertArticleDraftActor(actor: ArticleDraftActor): void {
  actor.signal?.throwIfAborted();
  if (!actor.viewerDid || actor.viewerDid !== actor.session.did) throw new Error("Your Account Changed. Reopen Articles.");
  actor.assertCurrentActor?.();
}
export class ArticleDraftRequestError extends Error {
  constructor(message: string, public readonly status: number, public readonly code?: string) { super(message); }
}
export function validateArticleDraftPayload(value: unknown): ArticleDraftPayload {
  const draft = value as ArticleDraftPayload;
  if (!draft || !uuid.test(draft.id) || ![draft.title, draft.markdown, draft.excerpt, draft.path, draft.publicationUri, draft.createdAt, draft.updatedAt].every(value => typeof value === "string") || !Number.isFinite(Date.parse(draft.createdAt)) || !Number.isFinite(Date.parse(draft.updatedAt)) || !Array.isArray(draft.tags) || !draft.tags.every(value => typeof value === "string") || !Array.isArray(draft.assets)) throw new Error("Invalid Private Article Draft.");
  for (const value of [draft.coverAssetId, draft.publishedUri, draft.scheduleRecordKey, draft.scheduleUri, draft.publishingRecordKey, draft.publicationClaimId]) if (value !== undefined && typeof value !== "string") throw new Error("Invalid Private Article Draft Metadata.");
  if (draft.publicationClaimId !== undefined && !uuid.test(draft.publicationClaimId)) throw new Error("Invalid Article Publication Claim.");
  if (draft.assets.some(asset => !asset || !uuid.test(asset.id) || ![asset.alt, asset.name, asset.mimeType].every(value => typeof value === "string") || !Number.isFinite(asset.width) || !Number.isFinite(asset.height) || asset.width < 0 || asset.height < 0)) throw new Error("Invalid Private Article Draft Assets.");
  if (new TextEncoder().encode(JSON.stringify(draft)).length > 512 * 1024) throw new Error("This Private Article Draft Exceeds the Sync Size Limit. Your Local Copy Is Safe.");
  return draft;
}
function document(value: unknown, expectedId?: string): ArticleDraftDocument {
  const result = value as ArticleDraftDocument;
  if (!result || !uuid.test(result.draftId) || expectedId && expectedId !== result.draftId || !Number.isSafeInteger(result.revision) || result.revision < 1 || typeof result.deleted !== "boolean" || !uuid.test(result.mutationId) || !Number.isFinite(Date.parse(result.updatedAt)) || !Array.isArray(result.attachments)) throw new Error("Invalid Private Article Draft Response.");
  if (result.deleted) {
    if (result.draft !== undefined || result.attachments.length) throw new Error("Invalid Deleted Article Draft Response.");
  } else {
    if (validateArticleDraftPayload(result.draft).id !== result.draftId) throw new Error("Private Article Draft Identity Does Not Match.");
    const ids = new Set<string>();
    for (const binding of result.attachments) {
      const asset = binding.asset;
      if (!uuid.test(binding.localAssetId) || ids.has(binding.localAssetId) || !asset || !uuid.test(asset.assetId) || asset.state !== "ready" || typeof asset.name !== "string" || typeof asset.contentType !== "string" || !/^[\w.+-]+\/[\w.+-]+$/.test(asset.contentType) || !["photo", "video", "gif", "poster", "caption"].includes(asset.kind) || !Number.isSafeInteger(asset.byteLength) || asset.byteLength <= 0 || !/^[0-9a-f]{64}$/.test(asset.sha256)) throw new Error("Invalid Private Article Draft Attachment.");
      ids.add(binding.localAssetId);
    }
    if (result.draft!.assets.some(asset => !ids.has(asset.id)) || ids.size !== result.draft!.assets.length) throw new Error("Private Article Draft Attachments Are Incomplete.");
  }
  return result;
}
async function request(actor: ArticleDraftActor, method: string, init: RequestInit = {}, missing = false): Promise<unknown> {
  assertArticleDraftActor(actor);
  const timeout = new AbortController(), timer = setTimeout(() => timeout.abort(new Error("Article Draft Sync Timed Out. Your Local Copy Is Safe. Retry to Reconcile.")), 30_000);
  const signal = actor.signal ? AbortSignal.any([actor.signal, timeout.signal]) : timeout.signal;
  try {
    return await awaitWithAbort((async () => {
      const response = await gatewayFetch(actor.session, `${prefix}${method}`, { ...init, signal, cache: "no-store", redirect: "error" });
      assertArticleDraftActor(actor);
      if (missing && response.status === 404) return undefined;
      const value = await response.json();
      assertArticleDraftActor(actor);
      if (!response.ok) throw new ArticleDraftRequestError(response.status === 409 ? "This Article Draft Changed on Another Device. Your Local Copy Is Safe." : "Private Article Draft Sync Failed. Your Local Copy Is Safe. Retry.", response.status, typeof value.error === "string" ? value.error : undefined);
      return value;
    })(), signal);
  } finally { clearTimeout(timer); assertArticleDraftActor(actor); }
}
const json = (value: unknown): RequestInit => ({ method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(value) });
export async function getCloudArticleDraft(actor: ArticleDraftActor, draftId: string): Promise<ArticleDraftDocument | undefined> {
  if (!uuid.test(draftId)) throw new Error("Invalid Article Draft ID.");
  const result = await request(actor, `get?draftId=${encodeURIComponent(draftId)}`, {}, true);
  return result === undefined ? undefined : document(result, draftId);
}
export async function listCloudArticleDrafts(actor: ArticleDraftActor): Promise<ArticleDraftDocument[]> {
  const records = new Map<string, ArticleDraftDocument>(), cursors = new Set<string>();
  let cursor: string | undefined;
  for (let page = 0; page < 100; page++) {
    const query = new URLSearchParams({ limit: "20" }); if (cursor) query.set("cursor", cursor);
    const result = await request(actor, `list?${query}`) as { drafts: unknown[]; cursor?: string };
    if (!Array.isArray(result.drafts) || result.drafts.length > 20 || result.cursor !== undefined && !uuid.test(result.cursor)) throw new Error("Invalid Article Draft List Response.");
    for (const value of result.drafts) { const row = document(value); records.set(row.draftId, row); }
    cursor = result.cursor;
    if (!cursor) return [...records.values()];
    if (cursors.has(cursor)) throw new Error("Article Draft Pagination Repeated. Your Local Copy Is Safe.");
    cursors.add(cursor);
  }
  throw new Error("Article Draft Pagination Did Not Finish.");
}
export async function putCloudArticleDraft(actor: ArticleDraftActor, input: ArticleDraftPut): Promise<ArticleDraftDocument> {
  validateArticleDraftPayload(input.draft);
  if (!uuid.test(input.mutationId) || !Number.isSafeInteger(input.expectedRevision) || input.expectedRevision < 0) throw new Error("Invalid Article Draft Mutation.");
  const result = document(await request(actor, "put", json(input)), input.draft.id);
  if (result.mutationId !== input.mutationId || result.deleted || result.revision !== input.expectedRevision + 1) throw new Error("Article Draft Save Could Not Be Confirmed. Retry to Reconcile.");
  return result;
}
export async function deleteCloudArticleDraft(actor: ArticleDraftActor, input: ArticleDraftDelete): Promise<ArticleDraftDocument> {
  if (!uuid.test(input.draftId) || !uuid.test(input.mutationId) || !Number.isSafeInteger(input.expectedRevision) || input.expectedRevision < 0) throw new Error("Invalid Article Draft Deletion.");
  const result = document(await request(actor, "delete", json(input)), input.draftId);
  if (!result.deleted || result.mutationId !== input.mutationId) throw new Error("Article Draft Deletion Could Not Be Confirmed. Retry to Reconcile.");
  return result;
}
