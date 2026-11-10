import { describe, expect, it } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import { editableProfileImageUrl, loadEditableProfile, saveEditableProfile, validateProfileImage, validateProfileText } from "@/lib/profileEditing";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";

const did = "did:plc:abcdefghijklmnopqrstuvwx";
const cid = "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3pt5y6okqac52uhicb2o54y5u";
const blob = { $type: "blob", ref: { $link: "bafkreigh2akiscaildcv6ubnt36d2bk6bj3fx3xpq6xfj2vgtz3yqjv2zy" }, mimeType: "image/png", size: 3 };
const original = { $type: "app.bsky.actor.profile", displayName: "Before", description: "Old bio", avatar: blob, banner: blob, labels: { $type: "com.atproto.label.defs#selfLabels", values: [{ val: "!no-unauthenticated" }] }, pinnedPost: { uri: `at://${did}/app.bsky.feed.post/one`, cid }, createdAt: "2025-01-01T00:00:00.000Z", customField: { keep: true } };
const snapshot = { record: original, cid };
function transport(response?: (path: string, body: unknown) => Response | Promise<Response>) {
  const requests: { path: string; body: unknown; contentType: string | null }[] = [];
  const session = { did, getTokenInfo: async () => ({ aud: "https://pds.test" }), fetchHandler: async (url: string, init?: RequestInit) => {
    const parsed = new URL(url, "https://pds.test");
    const path = parsed.pathname;
    const contentType = new Headers(init?.headers).get("content-type");
    const body = contentType?.includes("application/json") ? JSON.parse(await new Response(init?.body).text()) : init?.body;
    requests.push({ path, body, contentType });
    if (response) return response(path, body);
    if (path.endsWith("getRecord")) return Response.json({ uri: `at://${did}/app.bsky.actor.profile/self`, cid, value: original });
    if (path.endsWith("uploadBlob")) return Response.json({ blob });
    return Response.json({ uri: `at://${did}/app.bsky.actor.profile/self`, cid });
  } } as unknown as OAuthSession;
  return { session, requests };
}
const values = { displayName: "After", description: "New bio", expectedDid: did, snapshot };

describe("profile editing", () => {
  it("loads the canonical PDS record with its CID and unknown fields", async () => {
    const { session, requests } = transport();
    expect(JSON.parse(JSON.stringify(await loadEditableProfile(session)))).toEqual(snapshot);
    expect(requests[0].path).toBe("/xrpc/com.atproto.repo.getRecord");
  });
  it("treats only RecordNotFound as an absent profile", async () => {
    const missing = transport(() => Response.json({ error: "RecordNotFound" }, { status: 400 }));
    expect(await loadEditableProfile(missing.session)).toEqual({ record: { $type: "app.bsky.actor.profile" }, cid: null });
    const offline = transport(() => Response.json({ error: "InternalServerError", message: "Offline" }, { status: 500 }));
    await expect(loadEditableProfile(offline.session)).rejects.toThrow("Offline");
  });
  it("preserves other profile fields and images while using a CID compare-and-swap", async () => {
    const { session, requests } = transport();
    const saved = await saveEditableProfile({ session, ...values });
    expect(saved.record).toEqual({ ...original, displayName: "After", description: "New bio" });
    expect(original.displayName).toBe("Before");
    expect(requests).toHaveLength(1);
    expect(requests[0].body).toEqual({ repo: did, collection: "app.bsky.actor.profile", rkey: "self", record: saved.record, swapRecord: cid, validate: true });
  });
  it("uploads a replacement avatar and removes the banner without changing other fields", async () => {
    const { session, requests } = transport();
    const file = new File([new Uint8Array([1, 2, 3])], "avatar.png", { type: "image/png" });
    const saved = await saveEditableProfile({ session, ...values, avatar: file, banner: null });
    expect(requests.map(request => request.path)).toEqual(["/xrpc/com.atproto.repo.uploadBlob", "/xrpc/com.atproto.repo.putRecord"]);
    expect(requests[0].contentType).toBe("image/png");
    expect(JSON.parse(JSON.stringify(saved.record.avatar))).toEqual(blob);
    expect(saved.record).not.toHaveProperty("banner");
    expect(saved.record.customField).toEqual({ keep: true });
  });
  it("creates a missing profile only if the record is still absent", async () => {
    const { session, requests } = transport();
    await saveEditableProfile({ session, ...values, snapshot: { record: { $type: "app.bsky.actor.profile" }, cid: null } });
    expect(requests[0].body).toHaveProperty("swapRecord", null);
  });
  it("reports concurrent edits without retrying or overwriting", async () => {
    const { session, requests } = transport(() => Response.json({ error: "InvalidSwap" }, { status: 400 }));
    await expect(saveEditableProfile({ session, ...values })).rejects.toThrow("changed elsewhere");
    expect(requests).toHaveLength(1);
  });
  it("rejects account changes before writes and after uploads", async () => {
    const first = transport();
    await expect(saveEditableProfile({ session: first.session, ...values, expectedDid: "did:plc:other" })).rejects.toThrow("account changed");
    expect(first.requests).toHaveLength(0);
    let current = true;
    const second = transport(path => { current = false; return Response.json(path.endsWith("uploadBlob") ? { blob } : {}); });
    await expect(saveEditableProfile({ session: second.session, ...values, avatar: new File(["png"], "a.png", { type: "image/png" }), assertCurrent: () => { if (!current) throw new Error("Account changed"); } })).rejects.toThrow("Account changed");
    expect(second.requests).toHaveLength(1);
  });
  it("leaves the record unchanged when an image upload fails", async () => {
    const { session, requests } = transport(() => Response.json({ error: "BlobTooLarge", message: "Upload failed" }, { status: 400 }));
    await expect(saveEditableProfile({ session, ...values, banner: new File(["png"], "b.png", { type: "image/png" }) })).rejects.toThrow("Upload failed");
    expect(requests).toHaveLength(1);
    expect(original.banner).toEqual(blob);
  });
  it("surfaces permissions recovery for existing sessions", async () => {
    const { session } = transport(() => Response.json({ error: "insufficient_scope" }, { status: 403 }));
    await expect(saveEditableProfile({ session, ...values })).rejects.toThrow(SCOPE_RECOVERY_MESSAGE);
  });
  it("validates native grapheme, byte and image limits before sending requests", async () => {
    expect(() => validateProfileText("👩‍👩‍👧‍👦".repeat(64), "")).toThrow("640 UTF-8 bytes");
    expect(() => validateProfileText("a".repeat(65), "")).toThrow("64 characters");
    expect(() => validateProfileText("", "a".repeat(257))).toThrow("256 characters");
    expect(() => validateProfileText("é".repeat(64), "👍".repeat(256))).not.toThrow();
    expect(() => validateProfileImage(new File(["gif"], "a.gif", { type: "image/gif" }))).toThrow("JPEG or PNG");
    expect(() => validateProfileImage(new File([new Uint8Array(1_000_001)], "a.png", { type: "image/png" }))).toThrow("1 MB");
    const { session, requests } = transport();
    await expect(saveEditableProfile({ session, ...values, description: "a".repeat(257) })).rejects.toThrow("256 characters");
    expect(requests).toHaveLength(0);
  });
  it("does not send a request after cancellation", async () => {
    const { session, requests } = transport();
    const controller = new AbortController(); controller.abort();
    await expect(saveEditableProfile({ session, ...values, signal: controller.signal })).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it("builds public image URLs from the PDS audience for wire and SDK blob references", async () => {
    const { session } = transport();
    const expected = `https://pds.test/xrpc/com.atproto.sync.getBlob?${new URLSearchParams({ did, cid: blob.ref.$link })}`;
    expect(await editableProfileImageUrl(session, blob)).toBe(expected);
    const loaded = await loadEditableProfile(session);
    expect(await editableProfileImageUrl(session, loaded.record.avatar)).toBe(expected);
    expect(await editableProfileImageUrl(session, undefined)).toBeUndefined();
  });
});
