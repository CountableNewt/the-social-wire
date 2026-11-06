import { afterEach, beforeEach, describe, expect, it, spyOn } from "bun:test";
import { AppBskyEmbedVideo, BlobRef } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { forgetBlueskyVideo, uploadBlueskyVideo } from "@/lib/blueskyVideoUpload";

const cid = "bafyreihdwdcefgh4dqkjv67uzcmw7ojee6xedzdetojuzjevtenxquvyku";
function video(): Blob {
  const box = (name: string, data: Uint8Array) => {
    const bytes = new Uint8Array(data.length + 8);
    new DataView(bytes.buffer).setUint32(0, bytes.length);
    bytes.set(new TextEncoder().encode(name), 4); bytes.set(data, 8); return bytes;
  };
  const timing = new Uint8Array(20), view = new DataView(timing.buffer);
  view.setUint32(12, 1000); view.setUint32(16, 20_000);
  return new Blob([box("ftyp", new Uint8Array(8)), box("moov", box("mvhd", timing))], { type: "video/mp4" });
}
function setup(options: {
  did?: string; processing?: boolean; failed?: boolean; alreadyExists?: boolean; foreignPoll?: boolean;
  invalidRef?: boolean; invalidPlan?: boolean; invalidReceipt?: boolean; oversizedOutput?: boolean;
  failPartOnce?: boolean; finishLostOnce?: boolean; afterRequest?: () => void;
  failRequestOnce?: { method: string; status: number; error?: string };
} = {}) {
  const did = options.did ?? "did:plc:video-uploader";
  const calls: string[] = [], parts: number[] = [];
  let failedPart = false, failedRequest = false, finishLost = false;
  const session = {
    did,
    getTokenInfo: async () => ({ aud: "https://viewer.pds.example" }),
    fetchHandler: async (path: string) => {
      const method = new URL(path, "https://viewer.pds.example").pathname.split("/").at(-1)!;
      expect(method).toBe("com.atproto.server.getServiceAuth");
      return Response.json({ token: "service-token" });
    },
  } as unknown as OAuthSession;
  const blob = { $type: "blob", ref: { $link: options.invalidRef ? "not-a-cid" : cid }, mimeType: "video/mp4", size: options.oversizedOutput ? 100_000_001 : video().size };
  const job = { jobId: "processing", did, error: options.alreadyExists ? "already_exists" : undefined, state: options.processing ? "JOB_STATE_PROCESSING" : options.failed ? "JOB_STATE_FAILED" : "JOB_STATE_COMPLETED", blob };
  const fetchSpy = spyOn(globalThis, "fetch").mockImplementation(Object.assign(async (input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    const url = new URL(String(input));
    expect(url.origin).toBe("https://video.bsky.app"); expect(init?.credentials).toBe("omit"); expect(init?.redirect).toBe("error");
    const method = url.pathname.split("/").at(-1)!; calls.push(method); options.afterRequest?.();
    if (options.failRequestOnce?.method === method && !failedRequest) {
      failedRequest = true;
      return Response.json({ error: options.failRequestOnce.error ?? "ServiceFailure" }, { status: options.failRequestOnce.status });
    }
    if (method === "app.bsky.video.getUploadLimits") return Response.json({ canUpload: true });
    if (method === "app.bsky.video.startUpload") return Response.json({ jobId: "upload", partCount: options.invalidPlan ? 5 : Math.ceil(video().size / 32), partSizeBytes: 32 });
    if (method === "app.bsky.video.uploadPart") {
      const partNumber = Number(url.searchParams.get("partNumber")); parts.push(partNumber);
      if (options.failPartOnce && partNumber === 2 && !failedPart) { failedPart = true; throw new TypeError("Connection lost"); }
      return Response.json({ partNumber: options.invalidReceipt ? 9 : partNumber, sizeBytes: (init?.body as Blob).size });
    }
    if (method === "app.bsky.video.finishUpload") {
      if (options.finishLostOnce && !finishLost) { finishLost = true; throw new TypeError("Lost finish response"); }
      return Response.json({ jobStatus: job });
    }
    if (method === "app.bsky.video.getJobStatus") return Response.json({ jobStatus: { ...job, state: "JOB_STATE_COMPLETED", jobId: options.foreignPoll ? "different-processing-job" : job.jobId } });
    if (method === "app.bsky.video.abortUpload") return Response.json({ state: "aborted" });
    throw new Error(`Unexpected request ${method}`);
  }, { preconnect: globalThis.fetch.preconnect }));
  return { session, calls, parts, fetchSpy };
}
const oldEnv = process.env.NEXT_PUBLIC_APP_ENV;
beforeEach(() => { process.env.NEXT_PUBLIC_APP_ENV = "prod"; });
afterEach(() => { if (oldEnv === undefined) delete process.env.NEXT_PUBLIC_APP_ENV; else process.env.NEXT_PUBLIC_APP_ENV = oldEnv; });

describe("shared Bluesky video upload", () => {
  it("returns a valid SDK video blob without creating a post and reuses it after publication failure", async () => {
    const s = setup(), key = crypto.randomUUID();
    try {
      const args = { session: s.session, key, video: video() };
      const uploaded = await uploadBlueskyVideo(args);
      expect(uploaded).toBeInstanceOf(BlobRef);
      expect(AppBskyEmbedVideo.validateMain({ $type: "app.bsky.embed.video", video: uploaded }).success).toBe(true);
      expect((await uploadBlueskyVideo(args)).ref.toString()).toBe(cid);
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(1);
      forgetBlueskyVideo(s.session.did, key);
      await uploadBlueskyVideo(args);
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(2);
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("retries only the interrupted part and preserves confirmed parts", async () => {
    const s = setup({ failPartOnce: true }), args = { session: s.session, key: crypto.randomUUID(), video: video() };
    try {
      await expect(uploadBlueskyVideo(args)).rejects.toThrow("Connection lost");
      expect(s.parts).toEqual([1, 2]);
      await uploadBlueskyVideo(args);
      expect(s.parts).toEqual([1, 2, 2]);
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(1);
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("starts a fresh upload after a confirmed missing or expired reservation", async () => {
    for (const failRequestOnce of [
      { method: "app.bsky.video.uploadPart", status: 410 },
      { method: "app.bsky.video.uploadPart", status: 400, error: "UploadExpired" },
      { method: "app.bsky.video.finishUpload", status: 404 },
      { method: "app.bsky.video.finishUpload", status: 400, error: "UploadAborted" },
      { method: "app.bsky.video.getUploadStatus", status: 404 },
    ]) {
      const s = setup({ failRequestOnce, finishLostOnce: failRequestOnce.method.endsWith("getUploadStatus") });
      const args = { session: s.session, key: crypto.randomUUID(), video: video() };
      try {
        await expect(uploadBlueskyVideo(args)).rejects.toThrow("video service failed");
        await uploadBlueskyVideo(args);
        expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(2);
      } finally { s.fetchSpy.mockRestore(); }
    }
  });
  it("starts over after the processing job disappears instead of polling that ID forever", async () => {
    const s = setup({ processing: true, failRequestOnce: { method: "app.bsky.video.getJobStatus", status: 404 } });
    const args = { session: s.session, key: crypto.randomUUID(), video: video() };
    try {
      await expect(uploadBlueskyVideo(args)).rejects.toThrow("video service failed (404)");
      await uploadBlueskyVideo(args);
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(2);
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("preserves a reservation for auth, quota and transient server errors", async () => {
    for (const status of [401, 403, 429, 503]) {
      const s = setup({ failRequestOnce: { method: "app.bsky.video.uploadPart", status, error: "NotFound" } });
      const args = { session: s.session, key: crypto.randomUUID(), video: video() };
      try {
        await expect(uploadBlueskyVideo(args)).rejects.toThrow("video service failed");
        await uploadBlueskyVideo(args);
        expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(1);
      } finally { s.fetchSpy.mockRestore(); }
    }
  });
  it("does not adopt a different job returned while polling", async () => {
    const s = setup({ processing: true, foreignPoll: true });
    try { await expect(uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video() })).rejects.toThrow("Mismatched Video Processing Job"); }
    finally { s.fetchSpy.mockRestore(); }
  });
  it("rejects malformed processed blob references", async () => {
    const s = setup({ invalidRef: true });
    try { await expect(uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video() })).rejects.toThrow("Invalid Video Blob Reference"); }
    finally { s.fetchSpy.mockRestore(); }
  });
  it("aborts invalid plans and receipts before video processing", async () => {
    for (const options of [{ invalidPlan: true }, { invalidReceipt: true }]) {
      const s = setup(options);
      try {
        await expect(uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video() })).rejects.toThrow("invalid video upload");
        expect(s.calls.at(-1)).toBe("app.bsky.video.abortUpload");
        expect(s.calls).not.toContain("app.bsky.video.finishUpload");
      } finally { s.fetchSpy.mockRestore(); }
    }
  });
  it("fences account changes after the video service responds", async () => {
    let changed = false;
    const s = setup({ afterRequest: () => { changed = true; } });
    try {
      await expect(uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video(), assertCurrentAccount: () => { if (changed) throw new Error("Account changed"); } })).rejects.toThrow("Account changed");
      expect(s.calls).toEqual(["app.bsky.video.getUploadLimits"]);
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("does not reuse another account's upload with the same attachment key", async () => {
    const key = crypto.randomUUID();
    for (const did of ["did:plc:first-viewer", "did:plc:second-viewer"]) {
      const s = setup({ did });
      try { await uploadBlueskyVideo({ session: s.session, key, video: video() }); expect(s.calls).toContain("app.bsky.video.startUpload"); }
      finally { s.fetchSpy.mockRestore(); }
    }
  });
  it("rejects changed bytes under the same identity but accepts a recovered copy of the exact file", async () => {
    const s = setup(), key = crypto.randomUUID(), original = video();
    try {
      await uploadBlueskyVideo({ session: s.session, key, video: original });
      await uploadBlueskyVideo({ session: s.session, key, video: new Blob([original], { type: original.type }) });
      const changed = new Uint8Array(await original.arrayBuffer()); changed[8] = 1;
      await expect(uploadBlueskyVideo({ session: s.session, key, video: new Blob([changed], { type: original.type }) })).rejects.toThrow("Video Has Changed");
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(1);
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("expires completed jobs and uploads afresh rather than using stale blob grants", async () => {
    const s = setup(), args = { session: s.session, key: crypto.randomUUID(), video: video() };
    const now = Date.now();
    let timeSpy: ReturnType<typeof spyOn> | undefined;
    try {
      await uploadBlueskyVideo(args);
      timeSpy = spyOn(Date, "now").mockReturnValue(now + 31 * 60_000);
      await uploadBlueskyVideo(args);
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(2);
    } finally { timeSpy?.mockRestore(); s.fetchSpy.mockRestore(); }
  });
  it("rejects processed output that exceeds the video embed lexicon limit", async () => {
    const s = setup({ oversizedOutput: true });
    try { await expect(uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video() })).rejects.toThrow("Invalid Video Blob Reference"); }
    finally { s.fetchSpy.mockRestore(); }
  });
  it("uses the existing processed blob when the service reports already_exists", async () => {
    const s = setup({ failed: true, alreadyExists: true });
    try {
      expect((await uploadBlueskyVideo({ session: s.session, key: crypto.randomUUID(), video: video() })).ref.toString()).toBe(cid);
      expect(s.calls).not.toContain("app.bsky.video.getJobStatus");
    } finally { s.fetchSpy.mockRestore(); }
  });
  it("drops failed processing jobs so the same attachment can be retried", async () => {
    const key = crypto.randomUUID(), s = setup({ failed: true });
    try {
      await expect(uploadBlueskyVideo({ session: s.session, key, video: video() })).rejects.toThrow("could not process");
      await expect(uploadBlueskyVideo({ session: s.session, key, video: video() })).rejects.toThrow("could not process");
      expect(s.calls.filter(call => call === "app.bsky.video.startUpload")).toHaveLength(2);
    } finally { s.fetchSpy.mockRestore(); }
  });
});
