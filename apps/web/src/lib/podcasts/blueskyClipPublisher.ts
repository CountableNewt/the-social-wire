import { AppBskyEmbedVideo, AppBskyRichtextFacet, ComAtprotoRepoStrongRef, RichText, type AppBskyFeedPost } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { createOAuthAgent } from "@/lib/atprotoClient";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { BLUESKY_VIDEO_MAX_BYTES as BLUESKY_CLIP_MAX_BYTES, BLUESKY_VIDEO_MAX_SECONDS as BLUESKY_CLIP_MAX_SECONDS, validateBlueskyVideo as validateBlueskyClipVideo, uploadBlueskyVideo, forgetBlueskyVideo } from "../blueskyVideoUpload";
import { createAuthenticatedAppViewAgent } from "@/lib/blueskySocialClient";
import type { PodcastClip } from "./client";
import { PodcastClipAlreadyPublishedError } from "./podcastClipAlreadyPublishedError";
import { podcastClipPostRkey } from "./podcastClipPostRkey";
export { PodcastClipAlreadyPublishedError } from "./podcastClipAlreadyPublishedError";

export { BLUESKY_VIDEO_MAX_BYTES as BLUESKY_CLIP_MAX_BYTES, BLUESKY_VIDEO_MAX_SECONDS as BLUESKY_CLIP_MAX_SECONDS, validateBlueskyVideo as validateBlueskyClipVideo } from "../blueskyVideoUpload";
export type BlueskyPodcastClipPublishArgs = {
  session: OAuthSession;
  clip: PodcastClip;
  video: Blob;
  text: string;
  signal?: AbortSignal;
  assertCurrentAccount?: () => void;
  onProgress?: (status: string) => void;
};
const activePublishes = new Set<string>();

function guard(args: BlueskyPodcastClipPublishArgs) {
  args.signal?.throwIfAborted();
  args.assertCurrentAccount?.();
}
function progress(args: BlueskyPodcastClipPublishArgs, status: string) { guard(args); args.onProgress?.(status); guard(args); }
/** A deterministic post key reconciles retries after an ambiguous publication. */
export async function publishBlueskyPodcastClip(args: BlueskyPodcastClipPublishArgs): Promise<{ uri: string; cid: string }> {
  guard(args);
  const { session, clip, video, text } = args;
  const duration = clip.endSeconds - clip.startSeconds;
  const richText = new RichText({ text });
  if (!clip.id || clip.status !== "complete" || !Number.isFinite(duration) || clip.startSeconds < 0 || duration <= 0 || duration > BLUESKY_CLIP_MAX_SECONDS || !Number.isFinite(Date.parse(clip.createdAt))) throw new Error("Generate a ready clip of at most 10 minutes before publishing.");
  if (richText.graphemeLength > 300 || new TextEncoder().encode(text).length > 3000) throw new Error("Posts must contain at most 300 characters and 3,000 UTF-8 bytes.");
  await validateBlueskyClipVideo(video); guard(args);
  const network = getAtprotoNetwork();
  if (network.plcDirectory !== "https://plc.directory" || network.publicAppView !== "https://public.api.bsky.app" || network.appViewDid !== "did:web:api.bsky.app") throw new Error("Bluesky video publishing is unavailable in the isolated local sandbox.");
  const rkey = await podcastClipPostRkey(session.did, clip.id, clip.createdAt); guard(args);
  const key = `${session.did}/${rkey}`;
  if (activePublishes.has(key)) throw new Error("This clip is already being published.");
  activePublishes.add(key);
  const agent = createOAuthAgent(session);
  const alt = clip.title.slice(0, 1000);
  const uri = `at://${session.did}/app.bsky.feed.post/${rkey}`;
  async function existing(): Promise<{ uri: string; cid: string } | undefined> {
    guard(args);
    try {
      const { data } = await agent.com.atproto.repo.getRecord({ repo: session.did, collection: "app.bsky.feed.post", rkey }, { signal: args.signal }); guard(args);
      const record = data.value as AppBskyFeedPost.Record;
      const embed = record.embed;
      if (data.uri !== uri || !data.cid || !ComAtprotoRepoStrongRef.validateMain({ uri: data.uri, cid: data.cid }).success || record.$type !== "app.bsky.feed.post" || typeof record.text !== "string" || !Number.isFinite(Date.parse(record.createdAt)) || !AppBskyEmbedVideo.isMain(embed) || embed.alt !== alt || !embed.video || embed.video.mimeType !== "video/mp4" || !Number.isSafeInteger(embed.video.size) || embed.video.size <= 0 || embed.video.size > BLUESKY_CLIP_MAX_BYTES) throw new Error("The existing clip post could not be verified. Reload before trying again.");
      if (record.text !== text) throw new PodcastClipAlreadyPublishedError(data.uri, data.cid);
      return { uri: data.uri, cid: data.cid };
    } catch (error) {
      guard(args);
      if (error && typeof error === "object" && "error" in error && error.error === "RecordNotFound") return undefined;
      throw error;
    }
  }
  try {
    progress(args, "Checking Existing Post");
    const found = await existing();
    if (found) { forgetBlueskyVideo(session.did, rkey); progress(args, "Published"); return found; }
    await richText.detectFacets(createAuthenticatedAppViewAgent(session)); guard(args);
    const facets = richText.facets?.map(facet => ({ ...facet, features: facet.features.filter(feature => !AppBskyRichtextFacet.isMention(feature) || feature.did.startsWith("did:")) })).filter(facet => facet.features.length);
    const blob = await uploadBlueskyVideo({ session, video, key: rkey, signal: args.signal, assertCurrentAccount: args.assertCurrentAccount, onProgress: args.onProgress });
    guard(args);
    const record: AppBskyFeedPost.Record = { $type: "app.bsky.feed.post", text, createdAt: new Date().toISOString(), ...(facets?.length ? { facets } : {}), embed: { $type: "app.bsky.embed.video", video: blob, alt } };
    progress(args, "Publishing Post");
    try {
      const { data } = await agent.com.atproto.repo.createRecord({ repo: session.did, collection: "app.bsky.feed.post", rkey, record }, { signal: args.signal }); guard(args);
      if (data.uri !== uri || !data.cid) throw new Error("Bluesky returned an invalid post reference.");
      forgetBlueskyVideo(session.did, rkey); progress(args, "Published"); return { uri: data.uri, cid: data.cid };
    } catch (error) {
      guard(args);
      const recovered = await existing();
      if (recovered) { forgetBlueskyVideo(session.did, rkey); progress(args, "Published"); return recovered; }
      throw error;
    }
  } finally {
    activePublishes.delete(key);
  }
}
