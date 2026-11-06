import type * as Mediabunny from "mediabunny";
import { boundedVideoOutput, CompressedVideoTooLargeError } from "./boundedVideoOutput";
import { composerVideoCompressionPlan, COMPOSER_VIDEO_COMPRESSION_MAX_BYTES, type CompressionVideoMetadata } from "./composerVideoCompressionPlan";

export type VideoCompressionOptions = { signal?: AbortSignal; onProgress?: (progress: number) => void };
export type VideoCompressionEngine = Pick<typeof Mediabunny, "Input" | "Output" | "BlobSource" | "MP4" | "Mp4OutputFormat" | "StreamTarget" | "Conversion" | "Quality" | "canEncodeVideo" | "canEncodeAudio">;
const unsupported = "This Browser Cannot Compress This Video While Preserving Its Picture And Sound. Try A Supported Browser Or Choose A Smaller MP4.";

/** The media engine is loaded only when a selected video needs compression. No media leaves this browser. */
export async function compressComposerVideo(original: Blob, metadata: CompressionVideoMetadata, options: VideoCompressionOptions = {}): Promise<Blob> {
  options.signal?.throwIfAborted();
  composerVideoCompressionPlan(metadata, 0);
  return compressComposerVideoWithEngine(await import("mediabunny"), original, metadata, options);
}

/** An explicit engine parameter allows deterministic protocol tests without replacing module globals. */
export async function compressComposerVideoWithEngine(engine: VideoCompressionEngine, original: Blob, metadata: CompressionVideoMetadata, options: VideoCompressionOptions = {}): Promise<Blob> {
  const guard = () => options.signal?.throwIfAborted();
  guard();
  composerVideoCompressionPlan(metadata, 0);
  for (let attempt = 0; attempt < 2; attempt++) {
    guard();
    const input = new engine.Input({ source: new engine.BlobSource(original, { maxCacheSize: 8 * 1024 * 1024 }), formats: [engine.MP4] });
    let output: Mediabunny.Output | undefined;
    let conversion: Mediabunny.Conversion | undefined;
    let cancellation: Promise<void> | undefined;
    const abort = () => { if (conversion) cancellation = conversion.cancel().catch(() => {}); else input.dispose(); };
    options.signal?.addEventListener("abort", abort, { once: true });
    try {
      const [video, audio] = await Promise.all([input.getPrimaryVideoTrack(), input.getPrimaryAudioTrack()]);
      guard();
      if (!video || !await video.canDecode()) throw new Error(unsupported);
      const sourceWidth = await video.getDisplayWidth(), sourceHeight = await video.getDisplayHeight();
      guard();
      const sourceMetadata = { ...metadata, width: sourceWidth, height: sourceHeight };
      // Prefer AAC encoding when available, but keep exact AAC packets on browsers
      // without an AAC encoder (notably older Safari). Never silently discard audio.
      let audioOptions: Mediabunny.ConversionAudioOptions | undefined;
      let audioBitrate = 0;
      if (audio) {
        const numberOfChannels = Math.min(2, await audio.getNumberOfChannels());
        const quality = new engine.Quality({ bitrate: 128_000, bitrateMode: "constant" });
        const config = { numberOfChannels, sampleRate: 48_000, quality };
        if (await engine.canEncodeAudio("aac", config) && await audio.canDecode()) {
          audioOptions = { codec: "aac", ...config, forceTranscode: true };
          audioBitrate = 128_000;
        } else if (await audio.getCodec() === "aac") {
          const stats = await audio.computePacketStats();
          if (!Number.isFinite(stats.averageBitrate) || stats.averageBitrate <= 0) throw new Error(unsupported);
          audioBitrate = Math.ceil(stats.averageBitrate * 1.1);
          audioOptions = { codec: "aac" };
        } else throw new Error(unsupported);
      }
      guard();
      const frameStats = await video.computePacketStats(120);
      const frameRate = Number.isFinite(frameStats.averagePacketRate) && frameStats.averagePacketRate > 0 ? Math.min(30, frameStats.averagePacketRate) : 30;
      const plan = composerVideoCompressionPlan(sourceMetadata, audioBitrate, attempt);
      const quality = new engine.Quality({ bitrate: plan.videoBitrate, bitrateMode: "constant" });
      const videoOptions: Mediabunny.ConversionVideoOptions = { codec: "avc", width: plan.width, height: plan.height, fit: "contain", frameRate, quality, forceTranscode: true, allowTransformationMetadata: false };
      if (!await engine.canEncodeVideo("avc", { width: plan.width, height: plan.height, frameRate, quality })) throw new Error(unsupported);
      guard();
      const target = boundedVideoOutput(COMPOSER_VIDEO_COMPRESSION_MAX_BYTES);
      output = new engine.Output({ format: new engine.Mp4OutputFormat({ fastStart: false }), target: new engine.StreamTarget(target.writable, { chunked: true, chunkSize: 1024 * 1024 }) });
      conversion = await engine.Conversion.init({ input, output, tracks: "primary", video: videoOptions, audio: audioOptions, tags: {}, showWarnings: false });
      guard();
      if (!conversion.isValid || !conversion.utilizedTracks.includes(video) || audio && !conversion.utilizedTracks.includes(audio)) throw new Error(unsupported);
      conversion.onProgress = progress => { guard(); options.onProgress?.((attempt + Math.max(0, Math.min(progress, 1))) / 2); guard(); };
      await conversion.execute();
      guard();
      const blob = target.toBlob();
      if (!blob.size || blob.size > COMPOSER_VIDEO_COMPRESSION_MAX_BYTES) throw new CompressedVideoTooLargeError();
      options.onProgress?.(1);
      guard();
      return blob;
    } catch (error) {
      guard();
      if (!(error instanceof CompressedVideoTooLargeError) || attempt === 1) throw error;
    } finally {
      options.signal?.removeEventListener("abort", abort);
      await cancellation;
      if (conversion && conversion.state !== "done" && conversion.state !== "canceled") await conversion.cancel().catch(() => {});
      if (output && output.state !== "finalized" && output.state !== "canceled") await output.cancel().catch(() => {});
      input.dispose();
    }
  }
  throw new CompressedVideoTooLargeError();
}
