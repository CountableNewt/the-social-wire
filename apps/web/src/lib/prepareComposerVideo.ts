import type { ComposerVideo } from "./composerMedia";
import { validateBlueskyVideo } from "./blueskyVideoUpload";

export type ComposerVideoMetadata = { width: number; height: number; duration: number };

/** Object URLs stay out of persisted drafts and are revoked on every exit. */
export async function readComposerVideoMetadata(blob: Blob): Promise<ComposerVideoMetadata> {
  const url = URL.createObjectURL(blob);
  const element = document.createElement("video");
  element.preload = "metadata";
  try {
    return await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("Video Preview Timed Out. Retry Or Choose Another MP4.")), 15_000);
      const fail = () => { clearTimeout(timer); reject(new Error("This Video Cannot Be Previewed. Choose A Compatible MP4.")); };
      element.onerror = fail;
      element.onloadedmetadata = () => {
        clearTimeout(timer);
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

export async function prepareComposerVideo(video: ComposerVideo): Promise<ComposerVideo> {
  if (video.original.type && video.original.type !== "video/mp4") throw new Error("Choose An MP4 Video Of At Most 300 MB And 10 Minutes.");
  // Browsers sometimes omit MIME types for files dropped from disk. The MP4
  // validator checks bytes and duration before any preview or network request.
  const blob = video.original.type === "video/mp4" ? video.original : video.original.slice(0, video.original.size, "video/mp4");
  await validateBlueskyVideo(blob);
  const metadata = await readComposerVideoMetadata(blob);
  return { ...video, original: blob, blob, ...metadata, status: "ready", error: undefined };
}
