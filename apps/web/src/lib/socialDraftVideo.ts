import type { AppBskyDraftDefs } from "@atproto/api";
import type { ComposerVideo } from "./composerMedia";
import type { ThreadSegment } from "./blueskyThreadClient";
import { prepareComposerVideo } from "./prepareComposerVideo";
import { socialDraftToSegments } from "./socialDraftConversion";
import { assertDraftAttachmentActor, describeDraftAttachment, downloadDraftAttachment, isRetiredDraftAttachment, PRIVATE_DRAFT_FILE_LIMIT, uploadDraftAttachment, type DraftAttachmentActor, type DraftAttachmentManifest, type DraftAttachmentRef } from "./socialDraftAttachmentClient";

const privateVideoPattern = /^socialwire-private:\/\/v1\/video\/([0-9a-f-]{36})$/i;

export function privateVideoPath(video: ComposerVideo): string {
  if (!video.privateAttachment) throw new Error("Private Video Has Not Been Checkpointed.");
  return `socialwire-private://v1/video/${video.privateAttachment.video.assetId}`;
}

/** Match exact saved paths only; filenames and post positions cannot identify bytes. */
export function restoreLocalDraftVideo(draft: AppBskyDraftDefs.Draft, embed: AppBskyDraftDefs.DraftEmbedVideo, local?: ThreadSegment[]): ComposerVideo | undefined {
  if (embed.captions?.length) return;
  const matches = (local ?? []).flatMap(segment => {
    const video = segment.media;
    if (video?.kind !== "video" || !(video.original instanceof Blob)) return [];
    const references = segment.nativeDraftPost?.embedVideos?.filter(item => item.localRef.path.endsWith(`/${encodeURIComponent(video.id)}`));
    const paths = video.privateAttachment ? [privateVideoPath(video)] : references?.length ? references.map(item => item.localRef.path) : draft.deviceId ? [`socialwire://${draft.deviceId}/${encodeURIComponent(video.id)}`] : [];
    return paths.includes(embed.localRef.path) ? [video] : [];
  });
  const candidates = [...new Set(matches)];
  return candidates.length === 1 ? { ...candidates[0], alt: embed.alt ?? "" } : undefined;
}

export function videoToDraftEmbed(video: ComposerVideo, deviceId: string, previous?: AppBskyDraftDefs.DraftEmbedVideo[]): AppBskyDraftDefs.DraftEmbedVideo {
  const path = video.privateAttachment ? privateVideoPath(video) : `socialwire://${deviceId}/${encodeURIComponent(video.id)}`;
  const original = previous?.find(item => item.localRef.path === path || item.localRef.path.startsWith("socialwire://") && item.localRef.path.endsWith(`/${encodeURIComponent(video.id)}`));
  return { ...original, localRef: { ...original?.localRef, path: video.privateAttachment ? path : original?.localRef.path ?? path }, ...(original && original.alt === undefined && video.alt === "" ? {} : { alt: video.alt }) };
}

export function hasPrivateDraftVideos(draft?: AppBskyDraftDefs.Draft): boolean {
  return !!draft?.posts.some(post => post.embedVideos?.some(video => video.localRef.path.startsWith("socialwire-private:")));
}

export function privateDraftVideoRefs(draft: AppBskyDraftDefs.Draft): DraftAttachmentRef[] {
  const refs = new Map<string, DraftAttachmentRef>();
  for (const post of draft.posts) for (const video of post.embedVideos ?? []) {
    const path = video.localRef.path;
    if (!path.startsWith("socialwire-private:")) continue;
    const match = privateVideoPattern.exec(path);
    if (!match) throw new Error("Invalid Private Video Reference. Reattach The Video.");
    refs.set(path, { assetId: match[1], role: "video", ref: path });
  }
  return [...refs.values()];
}

/** Stable IDs and the exact upload bytes must be durable before the first request. */
export async function preparePrivateDraftVideos(actor: DraftAttachmentActor, segments: ThreadSegment[], checkpoint: (prepared: ThreadSegment[]) => Promise<void>): Promise<void> {
  let prepared = segments;
  for (const segment of segments) {
    const video = segment.media;
    if (video?.kind !== "video") continue;
    assertDraftAttachmentActor(actor);
    if (video.status !== "ready" || !video.blob || !video.width || !video.height || !video.duration) throw new Error("Video Preparation Is Incomplete. Retry Or Reattach The Video Before Saving.");
    if (video.blob.size > PRIVATE_DRAFT_FILE_LIMIT) throw new Error("Videos Over 100 MiB Are Saved Locally Only. Keep This Composer Open To Publish, Or Choose A Smaller Video To Sync The Draft.");
    const previous = video.privateAttachment?.ownerDid === actor.viewerDid ? video.privateAttachment.video : undefined;
    let asset = await describeDraftAttachment(video.blob, video.name, previous, "video");
    const replace = async () => {
      assertDraftAttachmentActor(actor);
      prepared = prepared.map(item => item.media?.kind === "video" && item.media.id === video.id && item.media.original === video.original && item.media.blob === video.blob ? { ...item, media: { ...item.media, privateAttachment: { ownerDid: actor.viewerDid, video: asset } } } : item);
      await checkpoint(prepared);
      assertDraftAttachmentActor(actor);
    };
    await replace();
    try { asset = await uploadDraftAttachment(actor, asset, video.blob); }
    catch (error) {
      if (!isRetiredDraftAttachment(error)) throw error;
      assertDraftAttachmentActor(actor);
      asset = { ...asset, assetId: crypto.randomUUID(), state: "reserved" };
      await replace();
      asset = await uploadDraftAttachment(actor, asset, video.blob);
    }
    await replace();
  }
}

/** Only a manifest's exact ready video reference can provide portable bytes. */
export async function restorePrivateDraftVideos(actor: DraftAttachmentActor, draft: AppBskyDraftDefs.Draft, manifest: DraftAttachmentManifest, local?: ThreadSegment[]): Promise<ThreadSegment[]> {
  assertDraftAttachmentActor(actor);
  if (manifest.deleted) throw new Error("Private Draft Attachments Were Deleted. Reattach The Video.");
  let recovered = [...(local ?? [])];
  for (const { ref: path, assetId } of privateDraftVideoRefs(draft)) {
    assertDraftAttachmentActor(actor);
    const available = recovered.filter(segment => segment.media?.kind === "video" && segment.media.privateAttachment?.ownerDid === actor.viewerDid && privateVideoPath(segment.media) === path && segment.media.original instanceof Blob && segment.media.blob instanceof Blob && segment.media.status === "ready");
    if (available.length === 1) continue;
    const matches = manifest.attachments.filter(item => item.assetId === assetId && item.ref === path && item.role === "video");
    const asset = matches[0]?.asset;
    if (matches.length !== 1 || !asset || asset.assetId !== assetId || asset.kind !== "video" || asset.state !== "ready" || asset.contentType !== "video/mp4") throw new Error("Private Draft Video Is Missing. Retry Or Reattach It On The Original Device.");
    const blob = await downloadDraftAttachment(actor, asset);
    assertDraftAttachmentActor(actor);
    const video = await prepareComposerVideo({ kind: "video", id: asset.assetId, name: asset.name, original: blob, alt: "", status: "preparing" });
    assertDraftAttachmentActor(actor);
    recovered = recovered.filter(segment => segment.media?.kind !== "video" || !segment.media.privateAttachment || privateVideoPath(segment.media) !== path);
    recovered.push({ id: crypto.randomUUID(), text: "", media: { ...video, privateAttachment: { ownerDid: actor.viewerDid, video: asset } } });
  }
  return socialDraftToSegments(draft, recovered);
}
