import { afterEach, expect, it, spyOn } from "bun:test";
import { loadCustomVerifications, parseVerificationPreferences, verificationIssuers, verificationLabel, type VerificationSubject } from "@/lib/socialVerification";
const issuer = "did:plc:abcdefghijklmnopqrstuvwx", did = "did:plc:bcdefghijklmnopqrstuvwxy";
const preferences = { includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] };
const subject: VerificationSubject = { did, handle: "person.test", displayName: "Person" };
const verification = { issuer, uri: `at://${issuer}/app.bsky.graph.verification/one`, subject: did, handle: "person.test", displayName: "Person" };
const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).forEach(restore => restore()));
it("shows valid Bluesky verification and distinguishes trusted verifiers", () => {
  expect(verificationLabel({ ...subject, verification: { verifiedStatus: "valid", trustedVerifierStatus: "none", verifications: [] } }, preferences, [])).toBe("Verified on Bluesky");
  expect(verificationLabel({ ...subject, verification: { verifiedStatus: "none", trustedVerifierStatus: "valid", verifications: [] } }, preferences, [])).toBe("Trusted Verifier on Bluesky");
  expect(verificationLabel({ ...subject, verification: { verifiedStatus: "invalid", trustedVerifierStatus: "invalid", verifications: [] } }, preferences, [])).toBeUndefined();
  expect(verificationLabel({ ...subject, verification: { verifiedStatus: "valid", trustedVerifierStatus: "none", verifications: [] } }, { ...preferences, includesBlueskyVerification: false }, [])).toBeUndefined();
});
it("accepts custom issuers only while subject, handle and display name still match", () => {
  expect(verificationLabel(subject, preferences, [verification])).toBe("Verified by a Custom Trusted Verifier");
  for (const patch of [{ subject: issuer }, { handle: "old.test" }, { displayName: "Previous Name" }, { issuer: did }]) expect(verificationLabel(subject, preferences, [{ ...verification, ...patch }])).toBeUndefined();
  expect(verificationLabel(subject, { ...preferences, trustedVerifierDIDs: [] }, [verification])).toBeUndefined();
});
it("lists only valid enabled issuers and deduplicates official and custom verifiers", () => {
  const official = { issuer, uri: verification.uri, isValid: true, createdAt: "2026-11-07T00:00:00Z" };
  const verified = { ...subject, verification: { verifiedStatus: "valid", trustedVerifierStatus: "none", verifications: [official, { ...official, issuer: did, isValid: false }] } };
  expect(verificationIssuers(verified, preferences, [verification, verification])).toEqual([issuer]);
  expect(verificationIssuers(verified, { includesBlueskyVerification: false, trustedVerifierDIDs: [] }, [verification])).toEqual([]);
  expect(verificationIssuers({ ...verified, verification: { ...verified.verification, verifiedStatus: "invalid" } }, preferences, [{ ...verification, handle: "old.test" }])).toEqual([]);
});
it("normalizes local preferences and removes duplicate or invalid verifier IDs", () => {
  expect(parseVerificationPreferences(JSON.stringify({ includesBlueskyVerification: false, trustedVerifierDIDs: [issuer, issuer, "bad", 1] }))).toEqual({ includesBlueskyVerification: false, trustedVerifierDIDs: [issuer] });
  expect(parseVerificationPreferences("broken")).toEqual({ includesBlueskyVerification: true, trustedVerifierDIDs: [] });
});
function mockPages(pages: unknown[]) {
  const requests: { url: string; init?: RequestInit }[] = [];
  const fetcher = spyOn(globalThis, "fetch").mockImplementation((async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({ url: String(input), init });
    return new Response(JSON.stringify(pages.shift()), { headers: { "content-type": "application/json" } });
  }) as typeof fetch); restores.push(() => fetcher.mockRestore()); return requests;
}
const doc = { id: issuer, service: [{ id: "#atproto_pds", type: "AtprotoPersonalDataServer", serviceEndpoint: "https://verifier-pds.test" }] };
const row = { uri: verification.uri, value: { $type: "app.bsky.graph.verification", subject: did, handle: subject.handle, displayName: subject.displayName, createdAt: "2026-11-06T00:00:00Z" } };
it("resolves the issuer PDS and paginates valid verification records without OAuth", async () => {
  const requests = mockPages([doc, { records: [row, { ...row, value: { subject: did } }], cursor: "next" }, { records: [] }]);
  expect(await loadCustomVerifications(issuer)).toEqual([verification]);
  expect(new URL(requests[2]!.url).searchParams.get("cursor")).toBe("next");
  expect(requests.every(request => request.init?.credentials === "omit" && !(request.init?.headers as Record<string, string>).Authorization)).toBe(true);
});
it("rejects mismatched DID documents, foreign records and repeated cursors", async () => {
  mockPages([{ ...doc, id: did }]);
  await expect(loadCustomVerifications(issuer)).rejects.toThrow("Did Not Match");
});
it("rejects foreign issuer records", async () => {
  mockPages([doc, { records: [{ ...row, uri: `at://${did}/app.bsky.graph.verification/one` }] }]);
  await expect(loadCustomVerifications(issuer)).rejects.toThrow("Another Account");
});
it("fails closed on repeated pagination without returning partial verification results", async () => {
  mockPages([doc, { records: [row], cursor: "same" }, { records: [], cursor: "same" }]);
  await expect(loadCustomVerifications(issuer)).rejects.toThrow("Repeated a Page Cursor");
});
