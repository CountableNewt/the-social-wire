import type { OAuthSession } from "@atproto/oauth-client-browser";
import type { DraftAttachment } from "@/lib/socialDraftAttachmentClient";
import type { ArticleDraft } from "./articleDraftTypes";

export type ArticleDraftActor = { session: OAuthSession; viewerDid: string; signal?: AbortSignal; assertCurrentActor?: () => void };
export type ArticleDraftPayload = Omit<ArticleDraft, "revision"> & { assets: NonNullable<ArticleDraft["assets"]> };
export type ArticleDraftDocument = { draftId: string; revision: number; deleted: boolean; mutationId: string; updatedAt: string; draft?: ArticleDraftPayload; attachments: { localAssetId: string; asset: DraftAttachment }[] };
export type ArticleDraftPut = { expectedRevision: number; mutationId: string; draft: ArticleDraftPayload; attachments: { localAssetId: string; assetId: string }[] };
export type ArticleDraftDelete = { draftId: string; expectedRevision: number; mutationId: string };
export type ArticleDraftPending = { kind: "put"; request: ArticleDraftPut; localRevision: number; assets: { localAssetId: string; asset: DraftAttachment }[] } | { kind: "delete"; request: ArticleDraftDelete };
export type ArticleDraftSyncState = { draftId: string; serverRevision: number; ackedLocalRevision: number; pending?: ArticleDraftPending; deleted?: boolean; deleteRequested?: boolean; conflict?: ArticleDraftDocument; recoveryDraft?: ArticleDraft; assets?: { localAssetId: string; asset: DraftAttachment }[] };
