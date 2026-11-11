import { afterEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Gateway from "@/lib/socialWireGatewayClient";
import { ArticleDraftRequestError, deleteCloudArticleDraft, getCloudArticleDraft, listCloudArticleDrafts, putCloudArticleDraft } from "@/lib/articles/articleDraftCloudClient";
import type { ArticleDraftActor, ArticleDraftDocument, ArticleDraftPut } from "@/lib/articles/articleDraftCloudTypes";
const did = "did:plc:alice", id = "11111111-1111-4111-8111-111111111111", mutationId = "22222222-2222-4222-8222-222222222222";
const actor: ArticleDraftActor = { viewerDid: did, session: { did } as unknown as OAuthSession };
const draft = { id, title: "Private Article", markdown: "Private body", excerpt: "", path: "", tags: [], assets: [], publicationUri: "", createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-06T00:00:00Z" };
const input: ArticleDraftPut = { expectedRevision: 0, mutationId, draft, attachments: [] };
const document: ArticleDraftDocument = { draftId: id, revision: 1, deleted: false, mutationId, updatedAt: draft.updatedAt, draft, attachments: [] };
const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).reverse().forEach(restore => restore()));
function gateway(value: unknown, status = 200) { const request = spyOn(Gateway, "gatewayFetch").mockResolvedValue(Response.json(value, { status })); restores.push(() => request.mockRestore()); return request; }
it("sends private article metadata only through authenticated gateway XRPC with no-store", async () => {
  const request = gateway(document);
  expect(await putCloudArticleDraft(actor, input)).toEqual(document);
  expect(request.mock.calls[0][1]).toBe("/xrpc/app.thesocialwire.articleDraft.put");
  expect(request.mock.calls[0][2]).toMatchObject({ method: "POST", cache: "no-store", redirect: "error" });
  expect(JSON.parse(request.mock.calls[0][2]!.body as string)).toEqual(input);
  expect(request.mock.calls[0][0]).toBe(actor.session);
});
it("accepts absent remote drafts and explicit deletion tombstones", async () => {
  const request = gateway({ error: "NotFound" }, 404);
  expect(await getCloudArticleDraft(actor, id)).toBeUndefined();
  const tombstone = { ...document, deleted: true, draft: undefined, revision: 2 };
  request.mockResolvedValue(Response.json(tombstone));
  expect((await deleteCloudArticleDraft(actor, { draftId: id, expectedRevision: 1, mutationId })).deleted).toBe(true);
});
it("rejects another account before requesting and late account changes after a response", async () => {
  const request = gateway(document);
  await expect(getCloudArticleDraft({ ...actor, viewerDid: "did:plc:bob" }, id)).rejects.toThrow("Account Changed");
  expect(request).not.toHaveBeenCalled();
  let current = true;
  request.mockImplementation(async () => { current = false; return Response.json(document); });
  await expect(getCloudArticleDraft({ ...actor, assertCurrentActor() { if (!current) throw new Error("Actor Retired"); } }, id)).rejects.toThrow("Actor Retired");
});
it("exposes CAS conflicts without converting them into write retries", async () => {
  const request = gateway({ error: "Conflict" }, 409);
  let failure: unknown;
  try { await putCloudArticleDraft(actor, input); } catch (error) { failure = error; }
  expect(failure).toBeInstanceOf(ArticleDraftRequestError);
  expect(failure).toMatchObject({ status: 409, code: "Conflict" });
  expect(request).toHaveBeenCalledTimes(1);
});
it("preserves the save operation, server reason, status and code", async () => {
  gateway({ error: "Quota", message: "Private article draft quota exceeded" }, 413);
  await expect(putCloudArticleDraft(actor, input)).rejects.toMatchObject({
    status: 413, code: "Quota", message: expect.stringContaining("Saving the private draft failed: Private article draft quota exceeded (HTTP 413 · Quota)"),
  });
});
it.each([null, "<html>Bad Gateway</html>", "not json"])("preserves HTTP errors when a proxy sends an unstructured response: %s", async body => {
  const request = gateway(null);
  request.mockResolvedValue(new Response(body, { status: 502 }));
  await expect(putCloudArticleDraft(actor, input)).rejects.toMatchObject({ status: 502, message: expect.stringContaining("HTTP 502") });
});
it("explains expired authentication and gateway-only errors", async () => {
  gateway({ error: "Private draft access denied" }, 401);
  await expect(putCloudArticleDraft(actor, input)).rejects.toThrow("Sign in again to restore draft access");
});
it("rejects mismatched acknowledgements and incomplete private asset bindings", async () => {
  const request = gateway({ ...document, mutationId: crypto.randomUUID() });
  await expect(putCloudArticleDraft(actor, input)).rejects.toThrow("Could Not Be Confirmed");
  request.mockResolvedValue(Response.json({ ...document, draft: { ...draft, assets: [{ id: mutationId, alt: "Cover", name: "cover.png", mimeType: "image/png", width: 2, height: 2 }] } }));
  await expect(getCloudArticleDraft(actor, id)).rejects.toThrow("Attachments Are Incomplete");
});
it("includes tombstones and rejects repeated list cursors", async () => {
  const tombstone = { ...document, deleted: true, draft: undefined };
  const request = gateway({ drafts: [tombstone] });
  expect(await listCloudArticleDrafts(actor)).toEqual([JSON.parse(JSON.stringify(tombstone))]);
  expect(request.mock.calls[0][1]).toContain("limit=20");
  request.mockImplementation(async () => Response.json({ drafts: [], cursor: id }));
  await expect(listCloudArticleDrafts(actor)).rejects.toThrow("Pagination Repeated");
});
