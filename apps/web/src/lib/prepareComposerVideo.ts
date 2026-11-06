import type { ComposerVideo } from "./composerMedia";
import { validateBlueskyVideo } from "./blueskyVideoUpload";
import { PRIVATE_DRAFT_FILE_LIMIT } from "./socialDraftAttachmentClient";

export type ComposerVideoMetadata = { width: number; height: number; duration: number };

/** Object URLs stay out of persisted drafts and are revoked on every exit. */
export async function readComposerVideoMetadata(blob: Blob, signal?: AbortSignal): Promise<ComposerVideoMetadata> {
  signal?.throwIfAborted();
  const url = URL.createObjectURL(blob);
  const element = document.createElement("video");
  element.preload = "metadata";
  try {
    return await new Promise((resolve, reject) => {
      const cleanup = () => { clearTimeout(timer); signal?.removeEventListener("abort", abort); };
      const abort = () => { cleanup(); reject(signal?.reason ?? new DOMException("Canceled", "AbortError")); };
      const timer = setTimeout(() => { cleanup(); reject(new Error("Video Preview Timed Out. Retry Or Choose Another MP4.")); }, 15_000);
      const fail = () => { cleanup(); reject(new Error("This Video Cannot Be Previewed. Choose A Compatible MP4.")); };
      signal?.addEventListener("abort", abort, { once: true });
      element.onerror = fail;
      element.onloadedmetadata = () => {
        cleanup();
        const { videoWidth: width, videoHeight: height, duration } = element;
        if (![width, height].every(value => Number.isSafeInteger(value) && value > 0 && value <= 20000) || !Number.isFinite(duration) || duration <= 0 || duration > 600) {
          reject(new Error("Choose A Video With Valid Dimensions And At Most 10 Minutes Long."));
          return;
        }
        resolve({ width, height, duration });
      };
      element.src = url;
    });
  } finally {
    element.onerror = null;
    element.onloadedmetadata = null;
    element.removeAttribute("src");
    element.load();
    URL.revokeObjectURL(url);
  }
}

export async function prepareComposerVideo(video: ComposerVideo, options: { signal?: AbortSignal; onProgress?: (progress: number) => void } = {}): Promise<ComposerVideo> {
  options.signal?.throwIfAborted();
  if (video.original.type && video.original.type !== "video/mp4") throw new Error("Choose An MP4 Video Of At Most 10 Minutes.");
  // Browsers sometimes omit MIME types for files dropped from disk. The MP4
  // validator checks bytes and duration before any preview or network request.
  let blob = video.original.type === "video/mp4" ? video.original : video.original.slice(0, video.original.size, "video/mp4");
  const sourceByteLength = blob.size;
  let compressed = false;
  // Inspect duration in small slices before attempting compression. The upload
  // validator continues to use its strict 300 MB default everywhere else.
  await validateBlueskyVideo(blob, Number.MAX_SAFE_INTEGER);
  options.signal?.throwIfAborted();
  let metadata = await readComposerVideoMetadata(blob, options.signal);
  if (sourceByteLength > PRIVATE_DRAFT_FILE_LIMIT) {
    options.onProgress?.(0);
    const { compressComposerVideo } = await import("./compressComposerVideo");
    options.signal?.throwIfAborted();
    const output = await compressComposerVideo(blob, metadata, options);
    options.signal?.throwIfAborted();
    if (output.size > 100_000_000 || output.size >= sourceByteLength) throw new Error("Video Could Not Be Compressed Below The Limit. Choose A Smaller Video.");
    await validateBlueskyVideo(output);
    const outputMetadata = await readComposerVideoMetadata(output, options.signal);
    if (Math.abs(outputMetadata.duration - metadata.duration) > 0.5 || Math.abs(outputMetadata.width / outputMetadata.height / (metadata.width / metadata.height) - 1) > 0.02) throw new Error("Video Compression Could Not Preserve Its Duration And Orientation. Choose Another Video.");
    blob = output;
    metadata = outputMetadata;
    compressed = true;
  }
  options.signal?.throwIfAborted();
  // Once output is validated, keep one prepared file in IndexedDB rather than
  // duplicating a potentially multi-gigabyte original. Failures never replace it.
  return { ...video, id: compressed ? crypto.randomUUID() : video.id, privateAttachment: compressed ? undefined : video.privateAttachment, original: blob, blob, ...metadata, sourceByteLength: video.sourceByteLength ?? sourceByteLength, preparationProgress: undefined, status: "ready", error: undefined };
}
