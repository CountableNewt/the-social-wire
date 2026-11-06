import { afterEach, expect, it, spyOn } from "bun:test";
import { prepareComposerVideo } from "@/lib/prepareComposerVideo";
import { mediaReady, missingMediaAlt, type ComposerVideo } from "@/lib/composerMedia";

const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).reverse().forEach(restore => restore()));
function mp4() {
  const bytes = new Uint8Array(48);
  const view = new DataView(bytes.buffer);
  view.setUint32(0, 12); bytes.set(new TextEncoder().encode("ftyp"), 4);
  view.setUint32(12, 36); bytes.set(new TextEncoder().encode("moov"), 16);
  view.setUint32(20, 28); bytes.set(new TextEncoder().encode("mvhd"), 24);
  view.setUint32(40, 1000); view.setUint32(44, 5000);
  return new Blob([bytes]);
}
const attachment = (original = mp4()): ComposerVideo => ({ kind: "video", id: "video-1", name: "clip.mp4", original, alt: "A penguin swimming", status: "preparing" });
function decoder(fail = false, duration = 5) {
  const element = document.createElement("video");
  Object.defineProperties(element, { videoWidth: { value: 720 }, videoHeight: { value: 1280 }, duration: { configurable: true, value: duration }, src: { configurable: true, set() { queueMicrotask(() => element.dispatchEvent(new window.Event(fail ? "error" : "loadedmetadata"))); } } });
  const load = spyOn(element, "load").mockImplementation(() => {});
  const create = spyOn(document, "createElement").mockReturnValue(element);
  const revoke = spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
  const url = spyOn(URL, "createObjectURL").mockReturnValue("blob:preview");
  restores.push(() => load.mockRestore(), () => create.mockRestore(), () => revoke.mockRestore(), () => url.mockRestore());
  return { element, revoke };
}
it("prepares a portrait MP4 with an omitted MIME type and retains bytes and alt text", async () => {
  const { element, revoke } = decoder();
  const input = attachment();
  const ready = await prepareComposerVideo(input);
  expect(ready).toMatchObject({ id: input.id, width: 720, height: 1280, duration: 5, alt: input.alt, status: "ready" });
  expect(ready.blob?.type).toBe("video/mp4");
  expect(await ready.blob!.arrayBuffer()).toEqual(await input.original.arrayBuffer());
  expect(mediaReady(ready)).toBe(true); expect(missingMediaAlt(ready)).toBe(false);
  expect(revoke).toHaveBeenCalledWith("blob:preview"); expect(element.getAttribute("src")).toBeNull();
});
it("revokes local URLs after decode failure", async () => {
  const { revoke } = decoder(true);
  await expect(prepareComposerVideo(attachment())).rejects.toThrow("Cannot Be Previewed");
  expect(revoke).toHaveBeenCalled();
});
it("rejects unsupported inputs before making a local preview", async () => {
  const create = spyOn(URL, "createObjectURL"); restores.push(() => create.mockRestore());
  await expect(prepareComposerVideo(attachment(new Blob(["not mp4"], { type: "video/webm" })))).rejects.toThrow("Choose An MP4");
  await expect(prepareComposerVideo(attachment(new Blob(["not mp4"], { type: "video/mp4" })))).rejects.toThrow();
  expect(create).not.toHaveBeenCalled();
});
it("blocks invalid metadata, preparation states, and overlong video descriptions", () => {
  const ready: ComposerVideo = { ...attachment(), blob: new Blob([new Uint8Array(48)], { type: "video/mp4" }), status: "ready", width: 720, height: 1280, duration: 5 };
  expect(mediaReady(ready)).toBe(true);
  for (const patch of [{ status: "preparing" as const }, { status: "failed" as const }, { width: 0 }, { height: Infinity }, { duration: NaN }, { duration: 601 }, { alt: "a".repeat(1001) }, { blob: undefined }]) expect(mediaReady({ ...ready, ...patch })).toBe(false);
  expect(missingMediaAlt({ ...ready, alt: " " })).toBe(true);
});
it("rejects browser metadata beyond the duration limit and cleans up", async () => {
  const { revoke } = decoder(false, 601);
  await expect(prepareComposerVideo(attachment())).rejects.toThrow("At Most 10 Minutes");
  expect(revoke).toHaveBeenCalled();
});

it("compresses oversized source files locally and stores only validated prepared bytes", async () => {
  decoder();
  const compression = await import("@/lib/compressComposerVideo");
  const output = new Blob([mp4()], { type: "video/mp4" });
  const compress = spyOn(compression, "compressComposerVideo").mockImplementation(async (_source, _metadata, options) => { options?.onProgress?.(0.5); return output; });
  restores.push(() => compress.mockRestore());
  const original = new Blob([mp4()], { type: "video/mp4" });
  Object.defineProperty(original, "size", { value: 400_000_000 });
  const progress: number[] = [];
  const ready = await prepareComposerVideo(attachment(original), { onProgress: value => progress.push(value) });
  expect(compress).toHaveBeenCalledTimes(1);
  expect(compress.mock.calls[0][0]).toBe(original);
  expect(ready.blob).toBe(output); expect(ready.original).toBe(output);
  expect(ready.id).not.toBe("video-1"); expect(ready.privateAttachment).toBeUndefined();
  expect(ready.sourceByteLength).toBe(400_000_000); expect(ready.status).toBe("ready");
  expect(ready.alt).toBe("A penguin swimming"); expect(progress).toEqual([0, 0.5]);
});
it("rejects overlong videos before compression", async () => {
  const compression = await import("@/lib/compressComposerVideo");
  const compress = spyOn(compression, "compressComposerVideo"); restores.push(() => compress.mockRestore());
  decoder(false, 601);
  const original = new Blob([mp4()], { type: "video/mp4" });
  Object.defineProperty(original, "size", { value: 150_000_000 });
  await expect(prepareComposerVideo(attachment(original))).rejects.toThrow("At Most 10 Minutes");
  expect(compress).not.toHaveBeenCalled();
});
it("rejects compression output that still exceeds the target without changing its source", async () => {
  decoder();
  const compression = await import("@/lib/compressComposerVideo");
  const output = new Blob([mp4()], { type: "video/mp4" });
  Object.defineProperty(output, "size", { value: 101_000_000 });
  const compress = spyOn(compression, "compressComposerVideo").mockResolvedValue(output); restores.push(() => compress.mockRestore());
  const original = new Blob([mp4()], { type: "video/mp4" });
  Object.defineProperty(original, "size", { value: 150_000_000 });
  const video = attachment(original);
  await expect(prepareComposerVideo(video)).rejects.toThrow("Could Not Be Compressed");
  expect(video.original).toBe(original); expect(video.status).toBe("preparing");
});
it("stops metadata preparation promptly on cancellation and revokes the URL", async () => {
  const { element, revoke } = decoder();
  let started!: () => void;
  const metadataStarted = new Promise<void>(resolve => { started = resolve; });
  Object.defineProperty(element, "src", { set() { started(); }, configurable: true });
  const controller = new AbortController();
  const pending = prepareComposerVideo(attachment(), { signal: controller.signal });
  await metadataStarted;
  controller.abort(new DOMException("Canceled", "AbortError"));
  await expect(pending).rejects.toThrow("Canceled"); expect(revoke).toHaveBeenCalled();
});
it("does not accept a compressed file that shortens the selected video", async () => {
  const { element } = decoder();
  let reads = 0;
  Object.defineProperty(element, "duration", { configurable: true, get: () => reads++ === 0 ? 5 : 3 });
  const compression = await import("@/lib/compressComposerVideo");
  const compress = spyOn(compression, "compressComposerVideo").mockResolvedValue(new Blob([mp4()], { type: "video/mp4" })); restores.push(() => compress.mockRestore());
  const original = new Blob([mp4()], { type: "video/mp4" });
  Object.defineProperty(original, "size", { value: 150_000_000 });
  const video = attachment(original);
  await expect(prepareComposerVideo(video)).rejects.toThrow("Could Not Preserve Its Duration");
  expect(video.original).toBe(original);
});
