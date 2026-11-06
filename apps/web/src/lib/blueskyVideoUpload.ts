import { AppBskyEmbedVideo, BlobRef, jsonToLex, type AppBskyVideoDefs } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { createOAuthAgent } from "./atprotoClient";
import { getAtprotoNetwork } from "./atprotoNetwork";

export const BLUESKY_VIDEO_MAX_BYTES = 300_000_000;
export const BLUESKY_VIDEO_MAX_SECONDS = 600;
// The service can accept larger source files, but published MP4 blobs must fit the embed lexicon.
const BLUESKY_VIDEO_EMBED_MAX_BYTES = 100_000_000;
const VIDEO_ORIGIN = "https://video.bsky.app";
export type BlueskyVideoUploadArgs = {
  session: OAuthSession;
  video: Blob;
  /** Stable, immutable attachment identity. Reuse on retry; change when replacing the file. */
  key: string;
  signal?: AbortSignal;
  assertCurrentAccount?: () => void;
  onProgress?: (status: string) => void;
};
type Upload = { jobId: string; partSizeBytes: number; partCount: number };
// Processing credentials and IDs stay in memory and are isolated by account.
const pendingJobs = new Map<string, { job: AppBskyVideoDefs.JobStatus; fingerprint: string; updatedAt: number }>();
const pendingUploads = new Map<string, { upload: Upload; nextPart: number; fingerprint: string }>();
const activeUploads = new Set<string>();
function cacheKey(did: string, key: string) { return JSON.stringify([did, key]); }

export function forgetBlueskyVideo(did: string, key: string): void {
  pendingJobs.delete(cacheKey(did, key));
}
function completedJob(job: AppBskyVideoDefs.JobStatus): boolean {
  return job.state === "JOB_STATE_COMPLETED" || job.error === "already_exists" && !!job.blob;
}
// Completed jobs are useful when post creation fails, but do not retain them indefinitely.
function pruneCompletedJobs() {
  const completed = [...pendingJobs.entries()].filter(([, value]) => completedJob(value.job)).sort((a, b) => b[1].updatedAt - a[1].updatedAt);
  for (const [index, [key, value]] of completed.entries()) {
    if (index >= 50 || Date.now() - value.updatedAt > 30 * 60_000) pendingJobs.delete(key);
  }
}
/** Hash fixed-size chunks so retries verify exact bytes without reading a 300MB file at once. */
async function sourceFingerprint(args: BlueskyVideoUploadArgs): Promise<string> {
  const digests: Uint8Array[] = [];
  for (let offset = 0; offset < args.video.size; offset += 2 * 1024 * 1024) {
    guard(args);
    const bytes = await args.video.slice(offset, offset + 2 * 1024 * 1024).arrayBuffer();
    digests.push(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)));
  }
  guard(args);
  const hashes = new Uint8Array(digests.length * 32);
  digests.forEach((digest, index) => hashes.set(digest, index * 32));
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", hashes));
  guard(args);
  return `${args.video.size}:${Array.from(digest, byte => byte.toString(16).padStart(2, "0")).join("")}`;
}
function guard(args: BlueskyVideoUploadArgs) {
  args.signal?.throwIfAborted();
  args.assertCurrentAccount?.();
}
function progress(args: BlueskyVideoUploadArgs, status: string) { guard(args); args.onProgress?.(status); guard(args); }
async function pause(signal?: AbortSignal) {
  signal?.throwIfAborted();
  await new Promise<void>((resolve, reject) => {
    const abort = () => { clearTimeout(timer); reject(signal?.reason ?? new DOMException("Canceled", "AbortError")); };
    const timer = setTimeout(() => { signal?.removeEventListener("abort", abort); resolve(); }, 1000);
    signal?.addEventListener("abort", abort, { once: true });
  });
}

/** Read MP4 metadata in small slices, without copying a 300MB video into memory. */
export async function validateBlueskyVideo(video: Blob): Promise<void> {
  if (video.type !== "video/mp4" || video.size < 32 || video.size > BLUESKY_VIDEO_MAX_BYTES) throw new Error("Choose an MP4 video of at most 300 MB.");
  const header = new Uint8Array(await video.slice(0, 12).arrayBuffer());
  if (new TextDecoder().decode(header.slice(4, 8)) !== "ftyp") throw new Error("The selected video is not a valid MP4.");
  async function findBox(start: number, end: number, wanted: string): Promise<{ start: number; end: number } | undefined> {
    for (let offset = start, count = 0; offset + 8 <= end && count < 10_000; count++) {
      const bytes = await video.slice(offset, offset + 16).arrayBuffer();
      const view = new DataView(bytes);
      const type = new TextDecoder().decode(new Uint8Array(bytes, 4, 4));
      const rawSize = view.getUint32(0);
      const size = rawSize === 1 ? Number(view.getBigUint64(8)) : rawSize === 0 ? end - offset : rawSize;
      const headerSize = rawSize === 1 ? 16 : 8;
      if (!Number.isSafeInteger(size) || size < headerSize || offset + size > end) throw new Error("The selected MP4 has invalid metadata.");
      if (type === wanted) return { start: offset + headerSize, end: offset + size };
      offset += size;
    }
  }
  const movie = await findBox(0, video.size, "moov");
  const timing = movie && await findBox(movie.start, movie.end, "mvhd");
  if (!timing || timing.end - timing.start < 20) throw new Error("The selected MP4 is missing duration metadata.");
  const view = new DataView(await video.slice(timing.start, Math.min(timing.end, timing.start + 32)).arrayBuffer());
  const version = view.getUint8(0);
  if (version > 1 || (version === 1 && view.byteLength < 32)) throw new Error("The selected MP4 has invalid duration metadata.");
  const scale = view.getUint32(version === 1 ? 20 : 12);
  const ticks = version === 1 ? Number(view.getBigUint64(24)) : view.getUint32(16);
  const seconds = ticks / scale;
  if (!scale || !Number.isFinite(seconds) || seconds <= 0 || seconds > BLUESKY_VIDEO_MAX_SECONDS) throw new Error("Bluesky videos must be at most 10 minutes long.");
}

/** Preprocess with the official Social App multipart API before creating any post. */
export async function uploadBlueskyVideo(args: BlueskyVideoUploadArgs): Promise<BlobRef> {
  guard(args);
  if (!args.key) throw new Error("Video Upload Requires An Attachment Identity.");
  await validateBlueskyVideo(args.video); guard(args);
  const network = getAtprotoNetwork();
  if (network.plcDirectory !== "https://plc.directory" || network.publicAppView !== "https://public.api.bsky.app" || network.appViewDid !== "did:web:api.bsky.app") throw new Error("Bluesky video publishing is unavailable in the isolated local sandbox.");
  const { session, video } = args;
  const key = cacheKey(session.did, args.key);
  const fingerprint = await sourceFingerprint(args);
  pruneCompletedJobs();
  const existingFingerprint = pendingJobs.get(key)?.fingerprint ?? pendingUploads.get(key)?.fingerprint;
  if (existingFingerprint && existingFingerprint !== fingerprint) throw new Error("This Video Has Changed. Remove It And Attach It Again Before Uploading.");
  if (activeUploads.has(key)) throw new Error("This Video Is Already Being Uploaded.");
  activeUploads.add(key);
  const agent = createOAuthAgent(session);
  async function request<T>(method: string, token?: string, body?: BodyInit, params?: Record<string, string>, contentType = "application/json", signal = args.signal): Promise<T> {
    guard(args);
    const url = new URL(`/xrpc/${method}`, VIDEO_ORIGIN);
    for (const [k, v] of Object.entries(params ?? {})) url.searchParams.set(k, v);
    const response = await fetch(url, { method: body === undefined ? "GET" : "POST", headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { "Content-Type": contentType } : {}) }, body, signal, credentials: "omit", redirect: "error" }); guard(args);
    if (!response.ok) {
      const details = await response.json().catch(() => undefined) as { error?: unknown } | undefined;
      guard(args);
      const error = typeof details?.error === "string" ? details.error : undefined;
      const code = error?.toLowerCase().replace(/[^a-z]/g, "");
      // A confirmed missing/expired reservation cannot be resumed. Preserve IDs
      // for network, auth, rate-limit and server failures so those retries are safe.
      const existingJob = ["app.bsky.video.uploadPart", "app.bsky.video.finishUpload", "app.bsky.video.getUploadStatus", "app.bsky.video.getJobStatus"].includes(method);
      const missing = response.status === 404 || response.status === 410 || !!code && ["notfound", "uploadnotfound", "uploadsessionnotfound", "jobnotfound", "uploadexpired", "jobexpired", "uploadaborted", "uploadfailed", "invalidjobid"].includes(code);
      if (existingJob && response.status >= 400 && response.status < 500 && ![401, 403, 429].includes(response.status) && missing) {
        pendingUploads.delete(key);
        pendingJobs.delete(key);
      }
      throw Object.assign(new Error(`Bluesky video service failed (${response.status}). Please retry.`), { status: response.status, error });
    }
    const data = await response.json(); guard(args); return data as T;
  }
  let upload: Upload | undefined;
  let uploadToken: string | undefined;
  let finished = false;
  try {
    let job = pendingJobs.get(key)?.job;
    if (!job) {
      const reservation = pendingUploads.get(key);
      if (!reservation) {
        progress(args, "Checking Upload Limits");
        const { data: limitsAuth } = await agent.com.atproto.server.getServiceAuth({ aud: "did:web:video.bsky.app", lxm: "app.bsky.video.getUploadLimits" }, { signal: args.signal }); guard(args);
        const limits = await request<{ canUpload: boolean; remainingDailyVideos?: number; remainingDailyBytes?: number }>("app.bsky.video.getUploadLimits", limitsAuth.token);
        if ([limits.remainingDailyVideos, limits.remainingDailyBytes].some(value => value !== undefined && (!Number.isSafeInteger(value) || value < 0)) || limits.canUpload !== true || (limits.remainingDailyVideos !== undefined && limits.remainingDailyVideos < 1) || (limits.remainingDailyBytes !== undefined && limits.remainingDailyBytes < video.size)) throw new Error("Your Bluesky video upload quota is unavailable or exhausted. Try again later.");
      }
      const info = await session.getTokenInfo(); guard(args);
      const pds = new URL(info.aud);
      if (pds.protocol !== "https:" || pds.username || pds.password) throw new Error("Your account has an invalid PDS endpoint.");
      const audience = `did:web:${pds.hostname}${pds.port ? `%3A${pds.port}` : ""}`;
      const { data: uploadAuth } = await agent.com.atproto.server.getServiceAuth({ aud: audience, lxm: "com.atproto.repo.uploadBlob", exp: Math.floor(Date.now() / 1000) + 1800 }, { signal: args.signal }); guard(args);
      uploadToken = uploadAuth.token;
      progress(args, "Uploading Video");
      upload = reservation?.upload ?? await request<Upload>("app.bsky.video.startUpload", uploadToken, JSON.stringify({ sizeBytes: video.size, mimeType: "video/mp4", name: `${args.key.replace(/[^A-Za-z0-9_-]/g, "").slice(0, 80) || "video"}.mp4` }));
      if (typeof upload.jobId !== "string" || !upload.jobId || upload.partCount > 10_000 || !Number.isSafeInteger(upload.partSizeBytes) || upload.partSizeBytes <= 0 || upload.partSizeBytes > BLUESKY_VIDEO_MAX_BYTES || upload.partCount !== Math.ceil(video.size / upload.partSizeBytes)) throw new Error("Bluesky returned an invalid video upload plan.");
      const state = reservation ?? { upload, nextPart: 1, fingerprint };
      pendingUploads.set(key, state);
      for (let partNumber = state.nextPart; partNumber <= upload.partCount; partNumber++) {
        const start = (partNumber - 1) * upload.partSizeBytes;
        const chunk = video.slice(start, Math.min(video.size, start + upload.partSizeBytes));
        const receipt = await request<{ partNumber: number; sizeBytes: number }>("app.bsky.video.uploadPart", uploadToken, chunk, { jobId: upload.jobId, partNumber: String(partNumber) }, "application/octet-stream");
        if (receipt.partNumber !== partNumber || receipt.sizeBytes !== chunk.size) {
          pendingUploads.delete(key);
          throw new Error("Bluesky returned an invalid video upload receipt.");
        }
        state.nextPart = partNumber + 1;
      }
      // Finish stores this credential for the later PDS blob upload. Refresh it
      // after transferring parts so slow uploads do not leave an expired grant.
      const { data: finishAuth } = await agent.com.atproto.server.getServiceAuth({ aud: audience, lxm: "com.atproto.repo.uploadBlob", exp: Math.floor(Date.now() / 1000) + 1800 }, { signal: args.signal }); guard(args);
      uploadToken = finishAuth.token;
      for (let attempt = 0; attempt < 60; attempt++) {
        try {
          const result = await request<{ jobStatus: AppBskyVideoDefs.JobStatus }>("app.bsky.video.finishUpload", uploadToken, JSON.stringify({ jobId: upload.jobId }));
          job = result.jobStatus;
          break;
        } catch (error) {
          guard(args);
          if (!pendingUploads.has(key)) throw error;
          const status = await request<{ state: string; jobStatus?: AppBskyVideoDefs.JobStatus }>("app.bsky.video.getUploadStatus", uploadToken, undefined, { jobId: upload.jobId });
          if (status.state === "completed" && status.jobStatus) { job = status.jobStatus; break; }
          if (["failed", "aborted", "expired"].includes(status.state)) { pendingUploads.delete(key); throw new Error("Bluesky could not finish this video upload. Please retry."); }
          if (status.state !== "finishing") throw error;
          await pause(args.signal); guard(args);
        }
      }
      if (!job) throw new Error("Video upload is still finishing. Retry to check the same upload.");
      if (job.did !== session.did || typeof job.jobId !== "string" || !job.jobId || typeof job.state !== "string") { pendingUploads.delete(key); throw new Error("Bluesky returned a video job for another account."); }
      pendingUploads.delete(key);
      finished = true;
      pendingJobs.set(key, { job, fingerprint, updatedAt: Date.now() });
      pruneCompletedJobs();
    }
    progress(args, "Processing Video");
    for (let attempt = 0; attempt < 900; attempt++) {
      guard(args);
      if (job.did !== session.did || !job.jobId) { pendingJobs.delete(key); throw new Error("Bluesky returned a video job for another account."); }
      if (job.state === "JOB_STATE_FAILED" && !completedJob(job)) { pendingJobs.delete(key); throw new Error("Bluesky could not process this video. Retry Or Replace The Video."); }
      if (completedJob(job)) break;
      await pause(args.signal); guard(args);
      const expectedJobId: string = job.jobId;
      const status: { jobStatus: AppBskyVideoDefs.JobStatus } = await request<{ jobStatus: AppBskyVideoDefs.JobStatus }>("app.bsky.video.getJobStatus", undefined, undefined, { jobId: expectedJobId });
      if (!status.jobStatus || status.jobStatus.jobId !== expectedJobId || status.jobStatus.did !== session.did) {
        pendingJobs.delete(key);
        throw new Error("Bluesky Returned A Mismatched Video Processing Job.");
      }
      job = status.jobStatus;
      pendingJobs.set(key, { job, fingerprint, updatedAt: Date.now() });
      pruneCompletedJobs();
    }
    if (!completedJob(job)) throw new Error("Video processing is not ready yet. Retry to check the same job.");
    if (!job.blob || job.blob.mimeType !== "video/mp4" || !Number.isSafeInteger(job.blob.size) || job.blob.size <= 0 || job.blob.size > BLUESKY_VIDEO_EMBED_MAX_BYTES) {
      pendingJobs.delete(key);
      throw new Error("Bluesky Returned An Invalid Video Blob Reference.");
    }
    try {
      const blob = jsonToLex(job.blob);
      if (!(blob instanceof BlobRef) || blob.size > BLUESKY_VIDEO_EMBED_MAX_BYTES || !AppBskyEmbedVideo.validateMain({ $type: "app.bsky.embed.video", video: blob }).success) throw new Error("Invalid Blob Reference");
      return blob;
    } catch {
      pendingJobs.delete(key);
      throw new Error("Bluesky Returned An Invalid Video Blob Reference.");
    }
  } finally {
    activeUploads.delete(key);
    if (upload && uploadToken && !finished && (args.signal?.aborted || !pendingUploads.has(key))) {
      pendingUploads.delete(key);
      // Cancellation releases the reservation only if the same account is current.
      try {
        args.assertCurrentAccount?.();
        await fetch(`${VIDEO_ORIGIN}/xrpc/app.bsky.video.abortUpload`, { method: "POST", headers: { Authorization: `Bearer ${uploadToken}`, "Content-Type": "application/json" }, body: JSON.stringify({ jobId: upload.jobId }), credentials: "omit", redirect: "error", signal: AbortSignal.timeout(5000) });
      } catch { /* The service expires reservations if cleanup is unavailable. */ }
    }
  }
}
