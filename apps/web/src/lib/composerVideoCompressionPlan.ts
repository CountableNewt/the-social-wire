export type CompressionVideoMetadata = { width: number; height: number; duration: number };
export const COMPOSER_VIDEO_COMPRESSION_TARGET_BYTES = 95_000_000;
export const COMPOSER_VIDEO_COMPRESSION_MAX_BYTES = 100_000_000;

/** Leave room for muxing overhead and rate-control overshoot; never trim a video to fit. */
export function composerVideoCompressionPlan(metadata: CompressionVideoMetadata, audioBitrate: number, attempt = 0) {
  if (![metadata.width, metadata.height].every(value => Number.isSafeInteger(value) && value > 0 && value <= 20000) || !Number.isFinite(metadata.duration) || metadata.duration <= 0 || metadata.duration > 600) throw new Error("Choose A Video With Valid Dimensions And At Most 10 Minutes Long.");
  if (!Number.isFinite(audioBitrate) || audioBitrate < 0) throw new Error("The Video's Audio Size Could Not Be Verified.");
  if (attempt !== 0 && attempt !== 1) throw new Error("Invalid Video Compression Attempt.");
  const longEdge = attempt === 0 ? 1280 : 960;
  const shortEdge = attempt === 0 ? 720 : 540;
  const scale = Math.min(1, longEdge / Math.max(metadata.width, metadata.height), shortEdge / Math.min(metadata.width, metadata.height));
  const videoBitrate = Math.floor(Math.min(5_000_000, COMPOSER_VIDEO_COMPRESSION_TARGET_BYTES * 8 * 0.9 / metadata.duration - audioBitrate) * (attempt === 0 ? 1 : 0.65));
  if (videoBitrate < 128_000) throw new Error("This Video's Audio Leaves Too Little Room To Compress It Safely. Choose A Smaller Video Or A Browser With AAC Encoding Support.");
  return {
    width: Math.max(2, Math.floor(metadata.width * scale / 2) * 2),
    height: Math.max(2, Math.floor(metadata.height * scale / 2) * 2),
    videoBitrate,
  };
}
