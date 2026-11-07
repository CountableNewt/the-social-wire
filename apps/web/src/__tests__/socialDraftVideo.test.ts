import { afterEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { lexicons } from "@atproto/api";
import type { ComposerVideo } from "@/lib/composerMedia";
import type { ThreadSegment } from "@/lib/blueskyThreadClient";
import { segmentsToSocialDraft, socialDraftToSegments, removeUnavailableDraftMedia } from "@/lib/socialDraftConversion";
import { hasPrivateDraftVideos, preparePrivateDraftVideos, privateDraftVideoRefs, restorePrivateDraftVideos } from "@/lib/socialDraftVideo";
import * as Attachments from "@/lib/socialDraftAttachmentClient";
import * as VideoPreparation from "@/lib/prepareComposerVideo";

const actor = { viewerDid: "did:plc:video-owner", session: { did: "did:plc:video-owner" } as unknown as OAuthSession };
const device = "6d1d2e3f-1234-4567-8901-123456789012";
const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).reverse().forEach(restore => restore()));

function video(id = "video"): ComposerVideo {
  const blob = new Blob([id], { type: "video/mp4" });
  return { kind: "video", id, name: `${id}.mp4`, original: blob, blob, width: 640, height: 360, duration: 30, alt: "Video Description", status: "ready" };
}
const segment = (media = video()): ThreadSegment => ({ id: media.id, text: "Caption", media });
function videoFileServer() {
  const files = new Map<string, { asset: Attachments.DraftAttachment; blob: Blob }>();
  const upload = spyOn(Attachments, "uploadDraftAttachment").mockImplementation(async (_actor, asset, blob) => {
    const ready = { ...asset, state: "ready" as const };
    files.set(asset.assetId, { asset: ready, blob });
    return ready;
  });
  const download = spyOn(Attachments, "downloadDraftAttachment").mockImplementation(async (_actor, asset) => files.get(asset.assetId)!.blob);
  const prepare = spyOn(VideoPreparation, "prepareComposerVideo").mockImplementation(async input => ({ ...input, blob: input.original, width: 640, height: 360, duration: 30, status: "ready" }));
  restores.push(() => upload.mockRestore(), () => download.mockRestore(), () => prepare.mockRestore());
  return { files, upload, download, prepare };
}

it("roundtrips native video paths, remote alt text and unknown metadata without exposing bytes", () => {
  const local = [segment()];
  const native = segmentsToSocialDraft(local, device);
  expect(() => lexicons.assertValidXrpcInput("app.bsky.draft.createDraft", { draft: native })).not.toThrow();
  expect(native.posts[0].embedVideos?.[0]).toEqual({ localRef: { path: `socialwire://${device}/video` }, alt: "Video Description" });
  expect(JSON.stringify(native)).not.toContain("blob:");
  const remote = { ...native, posts: [{ ...native.posts[0], embedVideos: [{ ...native.posts[0].embedVideos![0], alt: "Remote Description", future: { aspectRatio: { width: 640, height: 360 } } }] }] };
  const recovered = socialDraftToSegments(remote, local);
  expect(recovered[0].unavailableDraftMedia).toBe(false);
  expect(recovered[0].media?.kind === "video" && recovered[0].media.alt).toBe("Remote Description");
  expect(recovered[0].media?.kind === "video" && recovered[0].media.original).toBe(local[0].media?.kind === "video" && local[0].media.original);
  expect(segmentsToSocialDraft(recovered, device, remote)).toEqual(remote);
});

it("restores reordered video posts by exact paths rather than positions", () => {
  const local = [segment(video("first")), segment(video("second"))];
  const native = segmentsToSocialDraft(local, device);
  const remote = { ...native, posts: [native.posts[1], native.posts[0]] };
  const restored = socialDraftToSegments(remote, local);
  expect(restored.map(item => item.media?.kind === "video" && item.media.id)).toEqual(["second", "first"]);
});

it("preserves missing, foreign-device, ambiguous, mixed and captioned videos until explicit removal", () => {
  const local = [segment()];
  const native = segmentsToSocialDraft(local, device);
  const embed = native.posts[0].embedVideos![0];
  for (const post of [
    { text: "", embedVideos: [{ localRef: { path: "file:///private/video.mp4" } }] },
    { text: "", embedVideos: [{ localRef: { path: "socialwire://foreign/video" } }] },
    { text: "", embedVideos: [embed, embed] },
    { text: "", embedVideos: [embed], embedImages: [{ localRef: { path: "photo" } }] },
    { text: "", embedVideos: [embed], embedExternals: [{ uri: "https://example.com" }] },
    { text: "", embedVideos: [{ ...embed, captions: [{ lang: "en", content: "Do Not Drop Captions" }] }] },
  ]) {
    const remote = { ...native, posts: [post] };
    const recovered = socialDraftToSegments(remote, local);
    expect(recovered[0].unavailableDraftMedia).toBe(true);
    expect(segmentsToSocialDraft(recovered, device, remote)).toEqual(remote);
    expect(segmentsToSocialDraft([removeUnavailableDraftMedia(recovered[0])], device, remote).posts).toEqual([{ text: "" }]);
  }
  const ambiguous = socialDraftToSegments(native, [local[0], segment()]);
  expect(ambiguous[0].unavailableDraftMedia).toBe(true);
});

it("does not resurrect removed or replaced videos from native draft baselines", () => {
  const local = [segment()];
  const native = segmentsToSocialDraft(local, device);
  const restored = socialDraftToSegments(native, local)[0];
  expect(segmentsToSocialDraft([{ ...restored, media: undefined }], device, native).posts[0].embedVideos).toBeUndefined();
  const photo = { id: "photo", name: "photo.jpg", original: new Blob(["photo"]), alt: "Photo", status: "ready" as const };
  expect(segmentsToSocialDraft([{ ...restored, media: { kind: "photos", photos: [photo] } }], device, native).posts[0].embedVideos).toBeUndefined();
  expect(socialDraftToSegments({ ...native, posts: [{ text: "Removed" }] }, local)[0].media).toBeUndefined();
});

it("checkpoints video bytes and stable IDs before uploads and stops if persistence fails", async () => {
  const server = videoFileServer();
  let checkpoint: ThreadSegment[] = [];
  await expect(preparePrivateDraftVideos(actor, [segment()], async prepared => { checkpoint = prepared; throw new Error("Disk Full"); })).rejects.toThrow("Disk Full");
  expect(server.upload).not.toHaveBeenCalled();
  const saved = checkpoint[0].media;
  if (saved?.kind !== "video") throw new Error("Missing Video");
  expect(saved.privateAttachment?.video.sha256).toHaveLength(64);
  expect(saved.blob).toBeInstanceOf(Blob);
});

it("privately restores a video across devices and retains per-post descriptions", async () => {
  const server = videoFileServer();
  let local = [segment(video("first")), segment(video("second"))];
  await preparePrivateDraftVideos(actor, local, async prepared => { local = prepared; });
  const native = segmentsToSocialDraft(local, device);
  expect(hasPrivateDraftVideos(native)).toBe(true);
  const refs = privateDraftVideoRefs(native);
  expect(refs).toHaveLength(2);
  expect(refs.every(ref => ref.role === "video")).toBe(true);
  const remote = { ...native, posts: [{ ...native.posts[1], embedVideos: [{ ...native.posts[1].embedVideos![0], alt: "Remote Second" }] }, native.posts[0]] };
  const manifest = { draftId: "draft", revision: 1, deleted: false, attachments: refs.map(ref => ({ ...ref, asset: server.files.get(ref.assetId)!.asset })) };
  const restored = await restorePrivateDraftVideos(actor, remote, manifest);
  expect(restored.every(item => !item.unavailableDraftMedia)).toBe(true);
  expect(restored.map(item => item.media?.kind === "video" && item.media.alt)).toEqual(["Remote Second", "Video Description"]);
  expect(segmentsToSocialDraft(restored, device, remote)).toEqual(remote);
  expect(server.download).toHaveBeenCalledTimes(2);
  await restorePrivateDraftVideos(actor, remote, manifest, restored);
  expect(server.download).toHaveBeenCalledTimes(2);
});

it("does not fetch native paths or mismatched private manifests", async () => {
  const server = videoFileServer();
  let local = [segment()];
  await preparePrivateDraftVideos(actor, local, async prepared => { local = prepared; });
  const native = segmentsToSocialDraft(local, device);
  const ref = privateDraftVideoRefs(native)[0];
  const manifest = { draftId: "draft", revision: 1, deleted: false, attachments: [{ ...ref, ref: "wrong-path", asset: server.files.get(ref.assetId)!.asset }] };
  await expect(restorePrivateDraftVideos(actor, native, manifest)).rejects.toThrow("Missing");
  expect(server.download).not.toHaveBeenCalled();
  expect((await restorePrivateDraftVideos(actor, { posts: [{ text: "", embedVideos: [{ localRef: { path: "https://host/video.mp4" } }] }] }, manifest))[0].unavailableDraftMedia).toBe(true);
  expect(server.download).not.toHaveBeenCalled();
});

it("durably replaces retired video IDs and allocates new IDs for another account", async () => {
  const server = videoFileServer();
  let local = [segment()];
  await preparePrivateDraftVideos(actor, local, async prepared => { local = prepared; });
  const first = local[0].media;
  if (first?.kind !== "video") throw new Error("Missing Video");
  const previous = first.privateAttachment!.video.assetId;
  let persistedId = previous;
  server.upload.mockImplementation(async (_actor, asset) => {
    if (asset.assetId === previous) throw Object.assign(new Error("Expired"), { status: 409, error: "AssetRetired" });
    expect(persistedId).toBe(asset.assetId);
    return { ...asset, state: "ready" };
  });
  await preparePrivateDraftVideos(actor, local, async prepared => {
    local = prepared;
    const media = prepared[0].media;
    if (media?.kind === "video") persistedId = media.privateAttachment!.video.assetId;
  });
  expect(persistedId).not.toBe(previous);
  const foreign = { ...actor, viewerDid: "did:plc:other", session: { did: "did:plc:other" } as unknown as OAuthSession };
  const previousOwnerId = persistedId;
  await preparePrivateDraftVideos(foreign, local, async prepared => {
    local = prepared;
    const media = prepared[0].media;
    if (media?.kind === "video") persistedId = media.privateAttachment!.video.assetId;
  });
  expect(persistedId).not.toBe(previousOwnerId);
});

it("rejects cloud videos above the private limit before hashing or network requests", async () => {
  const server = videoFileServer();
  const oversized = video();
  Object.defineProperty(oversized.blob, "size", { value: Attachments.PRIVATE_DRAFT_FILE_LIMIT + 1 });
  await expect(preparePrivateDraftVideos(actor, [segment(oversized)], async () => {})).rejects.toThrow("Saved Locally Only");
  expect(server.upload).not.toHaveBeenCalled();
});

it("stops upload after account changes and retains the local video", async () => {
  const server = videoFileServer();
  const local = [segment()];
  let changed = false;
  await expect(preparePrivateDraftVideos({ ...actor, assertCurrentActor: () => { if (changed) throw new Error("Account Changed"); } }, local, async () => { changed = true; })).rejects.toThrow("Account Changed");
  expect(server.upload).not.toHaveBeenCalled();
  expect(local[0].media?.kind).toBe("video");
});
