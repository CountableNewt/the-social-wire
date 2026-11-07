import { describe, expect, it } from "bun:test";
import type { ConversionOptions } from "mediabunny";
import { compressComposerVideoWithEngine, type VideoCompressionEngine } from "@/lib/compressComposerVideo";
import { boundedVideoOutput, CompressedVideoTooLargeError } from "@/lib/boundedVideoOutput";
import { composerVideoCompressionPlan } from "@/lib/composerVideoCompressionPlan";

const metadata = { width: 1920, height: 1080, duration: 600 };
function fixture(options: { audio?: boolean; nativeAac?: boolean; audioCodec?: string; dropAudio?: boolean; videoEncoding?: boolean; oversizedAttempts?: number; onExecute?: () => void } = {}) {
  let inputs = 0, disposals = 0, executions = 0, cancellations = 0;
  const conversions: ConversionOptions[] = [], sourceOptions: unknown[] = [], formats: unknown[] = [];
  const video = { canDecode: async () => true, getDisplayWidth: async () => 1080, getDisplayHeight: async () => 1920, computePacketStats: async () => ({ averagePacketRate: 24 }) };
  const audio = { canDecode: async () => true, getNumberOfChannels: async () => 2, getCodec: async () => options.audioCodec ?? "aac", computePacketStats: async () => ({ averageBitrate: 192_000 }) };
  class Input {
    constructor() { inputs++; }
    async getPrimaryVideoTrack() { return video; }
    async getPrimaryAudioTrack() { return options.audio === false ? null : audio; }
    dispose() { disposals++; }
  }
  class BlobSource { constructor(_blob: Blob, config: unknown) { sourceOptions.push(config); } }
  class StreamTarget { constructor(public writable: WritableStream) {} }
  class Output {
    target: StreamTarget;
    constructor(config: { target: StreamTarget }) { this.target = config.target; }
    async cancel() {}
  }
  class Mp4OutputFormat { constructor(config: unknown) { formats.push(config); } }
  class Quality { constructor(public options: unknown) {} }
  const engine = {
    Input, BlobSource, StreamTarget, Output, Mp4OutputFormat, Quality, MP4: {},
    canEncodeAudio: async () => options.nativeAac !== false,
    canEncodeVideo: async () => options.videoEncoding !== false,
    Conversion: { async init(config: ConversionOptions) {
      conversions.push(config);
      const writer = (config.output.target as unknown as StreamTarget).writable.getWriter();
      const conversion = {
        state: "idle", isValid: true,
        utilizedTracks: options.audio === false || options.dropAudio ? [video] : [video, audio],
        onProgress: undefined as ((progress: number) => void) | undefined,
        async execute() {
          executions++; options.onExecute?.(); conversion.onProgress?.(0.5);
          if (executions <= (options.oversizedAttempts ?? 0)) await writer.write({ position: 100_000_000, data: new Uint8Array([1]) });
          await writer.write({ position: 0, data: new Uint8Array([1, 2, 3]) });
          await writer.close(); conversion.state = "done";
        },
        async cancel() { cancellations++; conversion.state = "canceled"; await writer.abort().catch(() => {}); },
      };
      return conversion;
    } },
  } as unknown as VideoCompressionEngine;
  return { engine, conversions, sourceOptions, formats, get inputs() { return inputs; }, get disposals() { return disposals; }, get executions() { return executions; }, get cancellations() { return cancellations; } };
}

describe("video compression budget", () => {
  it("budgets ten-minute audio/video below the target with muxing overhead margin", () => {
    expect(composerVideoCompressionPlan({ width: 3840, height: 2160, duration: 600 }, 128_000)).toEqual({ width: 1280, height: 720, videoBitrate: 1_012_000 });
  });
  it("keeps portrait framing and lowers resolution and bitrate on the second attempt", () => {
    const source = { width: 1080, height: 1920, duration: 120 };
    const first = composerVideoCompressionPlan(source, 128_000), second = composerVideoCompressionPlan(source, 128_000, 1);
    expect(first.width).toBe(720); expect(first.height).toBe(1280);
    expect(second.width).toBe(540); expect(second.height).toBe(960);
    expect(second.videoBitrate).toBeLessThan(first.videoBitrate);
  });
  it("avoids upscaling small sources and uses even encoded dimensions", () => {
    const plan = composerVideoCompressionPlan({ width: 641, height: 359, duration: 20 }, 0);
    expect(plan.width).toBe(640); expect(plan.height).toBe(358);
  });
  it("rejects invalid duration/metadata or excessive audio rather than truncating it", () => {
    for (const source of [{ width: 1, height: 1, duration: 601 }, { width: 1920, height: 1080, duration: NaN }, { width: 0, height: 10, duration: 1 }]) expect(() => composerVideoCompressionPlan(source, 0)).toThrow("10 Minutes");
    expect(() => composerVideoCompressionPlan(metadata, 2_000_000)).toThrow("Audio Leaves Too Little Room");
  });
});

describe("bounded MP4 output", () => {
  it("supports random-access header rewrites and zero-filled gaps", async () => {
    const output = boundedVideoOutput(10, 4), writer = output.writable.getWriter();
    await writer.write({ data: new Uint8Array([1, 2, 3, 4, 5, 6]), position: 0 });
    await writer.write({ data: new Uint8Array([9, 8, 7]), position: 2 });
    await writer.write({ data: new Uint8Array([10]), position: 9 });
    expect(output.byteLength).toBe(10); await writer.close();
    expect([...new Uint8Array(await output.toBlob().arrayBuffer())]).toEqual([1, 2, 9, 8, 7, 6, 0, 0, 0, 10]);
  });
  it("rejects writes beyond the byte limit before allocating output", async () => {
    const output = boundedVideoOutput(10, 4), writer = output.writable.getWriter();
    await expect(writer.write({ data: new Uint8Array([1]), position: 10 })).rejects.toBeInstanceOf(CompressedVideoTooLargeError);
    expect(output.byteLength).toBe(0); expect(() => output.toBlob()).toThrow("Did Not Finish");
  });
  it("does not return incomplete or aborted output", async () => {
    const output = boundedVideoOutput(10), writer = output.writable.getWriter();
    await writer.write({ data: new Uint8Array([1]), position: 0 });
    expect(() => output.toBlob()).toThrow("Did Not Finish"); await writer.abort();
    expect(() => output.toBlob()).toThrow("Did Not Finish");
  });
});

describe("browser video compression", () => {
  it("reads large Blobs lazily, bakes portrait orientation, preserves source FPS/audio, and produces MP4", async () => {
    const f = fixture(), progress: number[] = [], original = new Blob(["source"], { type: "video/mp4" });
    Object.defineProperty(original, "size", { value: 400_000_000 });
    const result = await compressComposerVideoWithEngine(f.engine, original, metadata, { onProgress: value => progress.push(value) });
    expect(result.type).toBe("video/mp4"); expect(result.size).toBe(3);
    expect(f.sourceOptions).toEqual([{ maxCacheSize: 8 * 1024 * 1024 }]); expect(f.formats).toEqual([{ fastStart: false }]);
    expect(f.conversions[0].video).toMatchObject({ codec: "avc", width: 720, height: 1280, frameRate: 24, forceTranscode: true, allowTransformationMetadata: false });
    expect(f.conversions[0].audio).toMatchObject({ codec: "aac", forceTranscode: true, numberOfChannels: 2, sampleRate: 48_000 });
    expect(f.conversions[0].tracks).toBe("primary"); expect(f.conversions[0].tags).toEqual({});
    expect(progress.at(-1)).toBe(1); expect(f.disposals).toBe(1);
  });
  it("copies AAC audio without requiring an AAC encoder", async () => {
    const f = fixture({ nativeAac: false });
    await compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata);
    expect(f.conversions[0].audio).toEqual({ codec: "aac" });
    const quality = (f.conversions[0].video as { quality: { options: { bitrate: number } } }).quality;
    expect(quality.options.bitrate).toBeLessThan(1_012_000);
  });
  it("never substitutes silent video when source audio cannot be preserved", async () => {
    for (const options of [{ nativeAac: false, audioCodec: "opus" }, { dropAudio: true }]) {
      const f = fixture(options);
      await expect(compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata)).rejects.toThrow("Preserving Its Picture And Sound");
      expect(f.executions).toBe(0); expect(f.disposals).toBe(1);
    }
  });
  it("supports videos without an audio track", async () => {
    const f = fixture({ audio: false }); await compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata);
    expect(f.conversions[0].audio).toBeUndefined(); expect(f.executions).toBe(1);
  });
  it("fails explicitly if AVC encoding is unavailable", async () => {
    const f = fixture({ videoEncoding: false });
    await expect(compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata)).rejects.toThrow("Supported Browser");
    expect(f.executions).toBe(0); expect(f.disposals).toBe(1);
  });
  it("retries oversize output once at lower bitrate and resolution", async () => {
    const f = fixture({ oversizedAttempts: 1 }); await compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata);
    expect(f.executions).toBe(2); expect(f.disposals).toBe(2); expect(f.conversions[1].video).toMatchObject({ width: 540, height: 960 });
  });
  it("rejects output after two oversized attempts instead of uploading or looping", async () => {
    const f = fixture({ oversizedAttempts: 2 });
    await expect(compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata)).rejects.toThrow("Still Exceeds");
    expect(f.executions).toBe(2); expect(f.disposals).toBe(2);
  });
  it("cancels conversion and disposes input when aborted", async () => {
    const controller = new AbortController(), f = fixture({ onExecute: () => controller.abort(new Error("Compression Canceled")) });
    await expect(compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata, { signal: controller.signal })).rejects.toThrow("Compression Canceled");
    expect(f.cancellations).toBeGreaterThan(0); expect(f.disposals).toBe(1);
  });
  it("honors pre-aborted operations without reading media", async () => {
    const f = fixture();
    await expect(compressComposerVideoWithEngine(f.engine, new Blob(["source"]), metadata, { signal: AbortSignal.abort() })).rejects.toThrow(); expect(f.inputs).toBe(0);
  });
});
