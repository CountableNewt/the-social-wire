import { afterEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Gateway from "@/lib/socialWireGatewayClient";
import { describeDraftAttachment, downloadDraftAttachment, uploadDraftAttachment, getDraftAttachmentManifest, putDraftAttachmentManifest, deleteDraftAttachmentManifest, PRIVATE_DRAFT_FILE_LIMIT, PRIVATE_DRAFT_UPLOAD_CHUNK_BYTES, type DraftAttachmentActor } from "@/lib/socialDraftAttachmentClient";

const actor: DraftAttachmentActor = { viewerDid: "did:plc:owner", session: { did: "did:plc:owner" } as unknown as OAuthSession };
const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).reverse().forEach(restore => restore()));
function transport(send: (path: string, init?: RequestInit) => Promise<Response>) {
  const mock = spyOn(Gateway, "gatewayFetch").mockImplementation(async (session, path, init) => { expect(session).toBe(actor.session); return send(path, init); });
  restores.push(() => mock.mockRestore());
  return mock;
}
const file = () => new Blob(["private bytes"], { type: "image/jpeg" });
it("allows a full native thread manifest and rejects anything beyond its 800 references", async () => {
  const refs = Array.from({ length: 800 }, (_, i) => ({ assetId: "12345678-1234-1234-1234-123456789abc", role: "original" as const, ref: `photo-${i}` }));
  const mock = transport(async (_path, init) => {
    expect(JSON.parse(String(init?.body)).attachments).toHaveLength(800);
    return Response.json({ draftId: "draft", revision: 1, deleted: false, attachments: [] });
  });
  await expect(putDraftAttachmentManifest(actor, "draft", 0, refs)).resolves.toMatchObject({ revision: 1 });
  await expect(putDraftAttachmentManifest(actor, "draft", 0, [...refs, { ...refs[0], ref: "extra" }])).rejects.toThrow("Invalid Private Draft Attachment Reference");
  expect(mock).toHaveBeenCalledTimes(1);
});
it("reserves metadata and uploads a Blob to the fixed authenticated gateway, with no browser Content-Length override", async () => {
  const blob = file(); const asset = await describeDraftAttachment(blob, "photo.jpg");
  const mock = transport(async (path, init) => {
    expect(path.startsWith("/xrpc/app.thesocialwire.socialDraft.")).toBe(true);
    expect(init?.cache).toBe("no-store"); expect(init?.redirect).toBe("error");
    if (path.endsWith("reserveAttachment")) {
      expect(JSON.parse(String(init?.body))).toEqual({ assetId: asset.assetId, name: asset.name, contentType: asset.contentType, byteLength: asset.byteLength, sha256: asset.sha256, kind: "photo" });
      return Response.json({ attachment: asset });
    }
    const headers = new Headers(init?.headers);
    expect(headers.get("X-Social-Draft-Upload-Id")).toBe(asset.assetId);
    expect(headers.has("Content-Length")).toBe(false);
    expect(headers.get("Content-Type")).toBe("application/octet-stream");
    expect(init?.body).toBe(blob);
    return Response.json({ attachment: { ...asset, state: "ready" } });
  });
  expect((await uploadDraftAttachment(actor, asset, blob)).state).toBe("ready"); expect(mock).toHaveBeenCalledTimes(2);
});
it("keeps stable asset IDs for exact file metadata and changes them when bytes or metadata change", async () => {
  const original = await describeDraftAttachment(file(), "p.jpg");
  expect((await describeDraftAttachment(file(), "p.jpg", original)).assetId).toBe(original.assetId);
  expect((await describeDraftAttachment(file(), "renamed.jpg", original)).assetId).not.toBe(original.assetId);
  expect((await describeDraftAttachment(new Blob(["different"], { type: "image/jpeg" }), "p.jpg", original)).assetId).not.toBe(original.assetId);
});
it("reserves stable IDs again after a lost upload response and recognizes an already ready file", async () => {
  const asset = await describeDraftAttachment(file(), "p.jpg"); let attempts = 0;
  const mock = transport(async path => {
    if (path.endsWith("reserveAttachment")) return Response.json({ attachment: { ...asset, state: attempts ? "ready" : "reserved" } });
    attempts++; throw new Error("Response lost");
  });
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toThrow("Response lost");
  await expect(uploadDraftAttachment(actor, asset, file())).resolves.toEqual({ ...asset, state: "ready" });
  expect(mock).toHaveBeenCalledTimes(3);
});
it("rejects pending uploads and mismatched upload acknowledgements", async () => {
  const asset = await describeDraftAttachment(file(), "p.jpg");
  const mock = transport(async () => Response.json({ attachment: asset }));
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toThrow("Pending");
  mock.mockImplementation(async () => Response.json({ attachment: { ...asset, sha256: "0".repeat(64) } }));
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toThrow("Does Not Match");
});
it.each(["bytes", "length", "mime"])("verifies authenticated downloaded %s", async mismatch => {
  const asset = { ...await describeDraftAttachment(file(), "p.jpg"), state: "ready" as const };
  transport(async () => new Response(mismatch === "bytes" ? "wronggg bytes" : mismatch === "length" ? "short" : "private bytes", { headers: { "Content-Type": mismatch === "mime" ? "image/png" : "image/jpeg" } }));
  await expect(downloadDraftAttachment(actor, asset)).rejects.toThrow("Verification Failed");
});
it("downloads original bytes without following URLs provided in attachment names", async () => {
  const asset = { ...await describeDraftAttachment(file(), "https://untrusted.invalid/private"), state: "ready" as const };
  const mock = transport(async path => { expect(path).toBe(`/xrpc/app.thesocialwire.socialDraft.getAttachment?assetId=${asset.assetId}`); return new Response(file()); });
  expect(await (await downloadDraftAttachment(actor, asset)).text()).toBe("private bytes"); expect(mock).toHaveBeenCalledTimes(1);
});
it("rejects a different account before sending credentials and discards late actor results", async () => {
  const mock = transport(async () => Response.json({ draftId: "draft", revision: 0, deleted: false, attachments: [] }));
  await expect(getDraftAttachmentManifest({ ...actor, viewerDid: "did:plc:other" }, "draft")).rejects.toThrow("Account Changed");
  expect(mock).not.toHaveBeenCalled();
  let current = true;
  mock.mockImplementation(async () => { current = false; return Response.json({ draftId: "draft", revision: 0, deleted: false, attachments: [] }); });
  await expect(getDraftAttachmentManifest({ ...actor, assertCurrentActor: () => { if (!current) throw new Error("Account Changed"); } }, "draft")).rejects.toThrow("Account Changed");
});
it("aborts even a gateway transport that ignores cancellation", async () => {
  transport(() => new Promise(() => {})); const controller = new AbortController();
  const pending = getDraftAttachmentManifest({ ...actor, signal: controller.signal }, "draft");
  controller.abort(new Error("Closed")); await expect(pending).rejects.toThrow("Closed");
});
it("uses revision CAS for shared-file manifests and explicit cleanup tombstones", async () => {
  const asset = { ...await describeDraftAttachment(file(), "p.jpg"), state: "ready" as const };
  const refs = [{ assetId: asset.assetId, role: "original" as const, ref: "opaque" }];
  transport(async (path, init) => {
    const body = JSON.parse(String(init?.body)); expect(body.expectedRevision).toBe(7);
    return Response.json({ draftId: body.draftId, revision: 8, deleted: path.endsWith("deleteManifest"), attachments: path.endsWith("deleteManifest") ? [] : refs.map(ref => ({ ...ref, asset })) });
  });
  expect((await putDraftAttachmentManifest(actor, "fork", 7, refs)).attachments[0].assetId).toBe(asset.assetId);
  expect((await deleteDraftAttachmentManifest(actor, "original", 7)).deleted).toBe(true);
});
it("surfaces CAS conflict and rejects oversized or invalid metadata before upload", async () => {
  transport(async () => new Response(null, { status: 409 }));
  await expect(putDraftAttachmentManifest(actor, "draft", 2, [])).rejects.toMatchObject({ status: 409 });
  const asset = await describeDraftAttachment(file(), "p.jpg");
  await expect(uploadDraftAttachment(actor, { ...asset, byteLength: PRIVATE_DRAFT_FILE_LIMIT + 1 }, file())).rejects.toThrow("Invalid");
  await expect(describeDraftAttachment(new Blob(["bytes"]), "p.jpg")).rejects.toThrow("Invalid");
});

it("maps only getManifest 404 to a first-write revision-zero manifest and preserves 503", async () => {
  const mock = transport(async () => new Response(null, { status: 404 }));
  await expect(getDraftAttachmentManifest(actor, "new-draft")).resolves.toEqual({ draftId: "new-draft", revision: 0, deleted: false, attachments: [] });
  await expect(putDraftAttachmentManifest(actor, "new-draft", 0, [])).rejects.toMatchObject({ status: 404 });
  mock.mockImplementation(async () => new Response(null, { status: 503 }));
  await expect(getDraftAttachmentManifest(actor, "new-draft")).rejects.toMatchObject({ status: 503 });
});
it("distinguishes retired asset responses from ordinary CAS conflicts without changing identity in the transport", async () => {
  const asset = await describeDraftAttachment(file(), "p.jpg");
  const mock = transport(async () => Response.json({ error: "AssetRetired" }, { status: 409 }));
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toMatchObject({ status: 409, error: "AssetRetired" });
  expect(mock).toHaveBeenCalledTimes(1);
  mock.mockImplementation(async () => Response.json({ error: "Conflict" }, { status: 409 }));
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toMatchObject({ status: 409, error: "Conflict" });
  expect((await describeDraftAttachment(file(), "p.jpg", asset)).assetId).toBe(asset.assetId);
});
it("identifies the failed attachment upload and preserves storage error details", async () => {
  const asset = await describeDraftAttachment(file(), "p.jpg");
  transport(async path => path.endsWith("reserveAttachment")
    ? Response.json({ attachment: asset })
    : Response.json({ error: "Unavailable", message: "Private draft storage unavailable" }, { status: 503 }));
  await expect(uploadDraftAttachment(actor, asset, file())).rejects.toMatchObject({
    status: 503, error: "Unavailable", message: expect.stringContaining("Uploading the draft attachment failed: Private draft storage unavailable (HTTP 503 · Unavailable)"),
  });
});
it("explains pending uploads without claiming another device edited the draft", async () => {
  transport(async () => Response.json({ error: "UploadPending", message: "Attachment upload pending" }, { status: 409 }));
  await expect(putDraftAttachmentManifest(actor, "draft", 2, [])).rejects.toThrow("Wait for the attachment upload to finish");
});

const largeFile = () => new Blob([new Uint8Array(5 * 1024 * 1024).fill(37)], { type: "video/mp4" });
it("uploads files above the Function body limit as sequential bounded parts before completing", async () => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4", undefined, "video");
  const parts: number[] = [];
  transport(async (path, init) => {
    if (path.endsWith("reserveAttachment")) return Response.json({ attachment: asset });
    if (path.endsWith("completeAttachmentUpload")) {
      expect(parts).toEqual([0, 1]); expect(JSON.parse(String(init?.body))).toEqual({ assetId: asset.assetId });
      return Response.json({ attachment: { ...asset, state: "ready" } });
    }
    expect(path.endsWith("uploadAttachmentChunk")).toBe(true);
    const headers = new Headers(init?.headers); const part = Number(headers.get("X-Social-Draft-Upload-Part"));
    expect(headers.get("X-Social-Draft-Upload-Id")).toBe(asset.assetId);
    expect(headers.get("Content-Type")).toBe("application/octet-stream"); expect(headers.has("Content-Length")).toBe(false);
    const body = init?.body as Blob;
    expect(body.size).toBe(part === 0 ? PRIVATE_DRAFT_UPLOAD_CHUNK_BYTES : blob.size - PRIVATE_DRAFT_UPLOAD_CHUNK_BYTES);
    expect(new Uint8Array(await body.arrayBuffer()).every(byte => byte === 37)).toBe(true);
    parts.push(part); return Response.json({ assetId: asset.assetId, part, received: true });
  });
  await expect(uploadDraftAttachment(actor, asset, blob)).resolves.toEqual({ ...asset, state: "ready" });
  expect(asset.state).toBe("reserved"); expect(blob.size).toBe(5 * 1024 * 1024);
});
it("replays identical parts with the same reservation after a lost response", async () => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4");
  const parts: number[] = []; let failed = false;
  transport(async (path, init) => {
    if (path.endsWith("reserveAttachment")) { expect(JSON.parse(String(init?.body)).assetId).toBe(asset.assetId); return Response.json({ attachment: asset }); }
    if (path.endsWith("completeAttachmentUpload")) return Response.json({ attachment: { ...asset, state: "ready" } });
    const headers = new Headers(init?.headers); expect(headers.get("X-Social-Draft-Upload-Id")).toBe(asset.assetId);
    const part = Number(headers.get("X-Social-Draft-Upload-Part")); parts.push(part);
    if (part === 1 && !failed) { failed = true; throw new Error("Response lost"); }
    return Response.json({ assetId: asset.assetId, part, received: true });
  });
  await expect(uploadDraftAttachment(actor, asset, blob)).rejects.toThrow("Response lost");
  await expect(uploadDraftAttachment(actor, asset, blob)).resolves.toMatchObject({ state: "ready" });
  expect(parts).toEqual([0, 1, 0, 1]); expect(asset.state).toBe("reserved");
});
it("stops large transfers when the actor changes after a part", async () => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4"); let current = true;
  const mock = transport(async path => {
    if (path.endsWith("reserveAttachment")) return Response.json({ attachment: asset });
    current = false; return Response.json({ assetId: asset.assetId, part: 0, received: true });
  });
  await expect(uploadDraftAttachment({ ...actor, assertCurrentActor: () => { if (!current) throw new Error("Account Changed"); } }, asset, blob)).rejects.toThrow("Account Changed");
  expect(mock).toHaveBeenCalledTimes(2); expect(asset.state).toBe("reserved");
});
it("cancels a large transfer even when its part transport ignores cancellation", async () => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4"); const controller = new AbortController();
  let started!: () => void; const partStarted = new Promise<void>(resolve => { started = resolve; });
  const mock = transport(async path => {
    if (path.endsWith("reserveAttachment")) return Response.json({ attachment: asset });
    started(); return new Promise<Response>(() => {});
  });
  const pending = uploadDraftAttachment({ ...actor, signal: controller.signal }, asset, blob);
  await partStarted; controller.abort(new Error("Closed"));
  await expect(pending).rejects.toThrow("Closed"); expect(mock).toHaveBeenCalledTimes(2); expect(asset.state).toBe("reserved");
});
it.each(["missing part", "finalize failure", "pending finalize", "mismatched finalize"])("keeps the local file recoverable after %s", async failure => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4"); let completed = false;
  transport(async (path, init) => {
    if (path.endsWith("reserveAttachment")) return Response.json({ attachment: asset });
    if (path.endsWith("uploadAttachmentChunk")) {
      if (failure === "missing part") return Response.json({});
      return Response.json({ assetId: asset.assetId, part: Number(new Headers(init?.headers).get("X-Social-Draft-Upload-Part")), received: true });
    }
    completed = true;
    if (failure === "finalize failure") return Response.json({ error: "Unavailable" }, { status: 503 });
    return Response.json({ attachment: failure === "pending finalize" ? asset : { ...asset, state: "ready", sha256: "0".repeat(64) } });
  });
  await expect(uploadDraftAttachment(actor, asset, blob)).rejects.toThrow();
  expect(completed).toBe(failure !== "missing part"); expect(asset.state).toBe("reserved");
  expect((await describeDraftAttachment(blob, "video.mp4", asset)).assetId).toBe(asset.assetId);
});
it("recognizes completed large uploads on retry after losing the completion response", async () => {
  const blob = largeFile(); const asset = await describeDraftAttachment(blob, "video.mp4"); let completed = false; let parts = 0;
  transport(async (path, init) => {
    if (path.endsWith("reserveAttachment")) {
      expect(JSON.parse(String(init?.body)).assetId).toBe(asset.assetId);
      return Response.json({ attachment: { ...asset, state: completed ? "ready" : "reserved" } });
    }
    if (path.endsWith("completeAttachmentUpload")) { completed = true; throw new Error("Completion response lost"); }
    parts++;
    return Response.json({ assetId: asset.assetId, part: Number(new Headers(init?.headers).get("X-Social-Draft-Upload-Part")), received: true });
  });
  await expect(uploadDraftAttachment(actor, asset, blob)).rejects.toThrow("Completion response lost");
  await expect(uploadDraftAttachment(actor, asset, blob)).resolves.toMatchObject({ assetId: asset.assetId, state: "ready" });
  expect(parts).toBe(2); expect(asset.state).toBe("reserved");
});
