import { Agent, AppBskyGraphVerification, type AppBskyActorDefs } from "@atproto/api";
import { getAtprotoNetwork } from "./atprotoNetwork";

export type VerificationPreferences = { includesBlueskyVerification: boolean; trustedVerifierDIDs: string[] };
export type VerificationSubject = { did: string; handle?: string; displayName?: string; verification?: AppBskyActorDefs.VerificationState };
export type CustomVerification = { issuer: string; subject: string; handle: string; displayName: string; uri: string };
export const DEFAULT_VERIFICATION_PREFERENCES: VerificationPreferences = { includesBlueskyVerification: true, trustedVerifierDIDs: [] };
export const VERIFICATION_STORAGE_PREFIX = "the-social-wire.trusted-verifiers.v1:";
export function isVerifierDID(value: string): boolean { return /^did:(?:plc:[a-z2-7]{24}|web:[^\s/?#]+)$/.test(value); }
export function parseVerificationPreferences(raw: string | null): VerificationPreferences {
  try {
    const value = JSON.parse(raw ?? "null");
    if (!value || typeof value !== "object") return DEFAULT_VERIFICATION_PREFERENCES;
    return { includesBlueskyVerification: value.includesBlueskyVerification !== false, trustedVerifierDIDs: [...new Set<string>(Array.isArray(value.trustedVerifierDIDs) ? value.trustedVerifierDIDs.filter((did: unknown): did is string => typeof did === "string" && isVerifierDID(did)) : [])] };
  } catch { return DEFAULT_VERIFICATION_PREFERENCES; }
}
export function verificationLabel(subject: VerificationSubject, preferences: VerificationPreferences, records: readonly CustomVerification[]): string | undefined {
  if (preferences.includesBlueskyVerification) {
    if (subject.verification?.trustedVerifierStatus === "valid") return "Trusted Verifier on Bluesky";
    if (subject.verification?.verifiedStatus === "valid") return "Verified on Bluesky";
  }
  const issuers = customVerificationIssuers(subject, preferences, records);
  return issuers.length ? "Verified by a Custom Trusted Verifier" : undefined;
}
function customVerificationIssuers(subject: VerificationSubject, preferences: VerificationPreferences, records: readonly CustomVerification[]): string[] {
  return records.filter(record => preferences.trustedVerifierDIDs.includes(record.issuer) && record.subject === subject.did && record.handle === subject.handle && record.displayName === (subject.displayName ?? "")).map(record => record.issuer);
}
export function verificationIssuers(subject: VerificationSubject, preferences: VerificationPreferences, records: readonly CustomVerification[]): string[] {
  const official = preferences.includesBlueskyVerification && subject.verification?.verifiedStatus === "valid"
    ? subject.verification.verifications.filter(verification => verification.isValid && isVerifierDID(verification.issuer)).map(verification => verification.issuer)
    : [];
  return [...new Set([...official, ...customVerificationIssuers(subject, preferences, records)])];
}
async function publicJSON(url: string, signal?: AbortSignal) {
  const response = await fetch(url, { signal, credentials: "omit", redirect: "error", headers: { Accept: "application/json" } });
  if (!response.ok) throw new Error(`Verifier Request Failed (${response.status}).`);
  return response.json();
}
export async function resolveVerifierProfile(actor: string, signal?: AbortSignal) {
  const value = actor.trim().replace(/^@/, "");
  if (!value) throw new Error("Enter a Verifier Handle or DID.");
  const { data } = await new Agent(getAtprotoNetwork().publicAppView).app.bsky.actor.getProfile({ actor: value }, { signal });
  if (!isVerifierDID(data.did)) throw new Error("The Verifier Returned an Invalid DID.");
  return data;
}
export async function loadCustomVerifications(issuer: string, signal?: AbortSignal): Promise<CustomVerification[]> {
  if (!isVerifierDID(issuer)) throw new Error("Invalid Verifier DID.");
  const network = getAtprotoNetwork();
  let didURL: string;
  if (issuer.startsWith("did:plc:")) didURL = `${network.plcDirectory}/${encodeURIComponent(issuer)}`;
  else {
    if (network.plcDirectory !== "https://plc.directory") throw new Error("Custom DID-Web Verifiers Are Unavailable in the Isolated Network.");
    const parts = issuer.slice(8).split(":").map(decodeURIComponent), host = parts.shift()!;
    didURL = `https://${host}/${parts.length ? `${parts.map(encodeURIComponent).join("/")}/did.json` : ".well-known/did.json"}`;
    const url = new URL(didURL);
    if (url.username || url.password || url.search || url.hash) throw new Error("Invalid Verifier DID-Web Address.");
  }
  const doc = await publicJSON(didURL, signal) as { id?: string; service?: { id?: string; type?: string; serviceEndpoint?: string }[] };
  if (doc.id !== issuer) throw new Error("Verifier DID Document Did Not Match Its Identity.");
  const service = doc.service?.find(item => (item.id === "#atproto_pds" || item.id === `${issuer}#atproto_pds`) && item.type === "AtprotoPersonalDataServer");
  if (!service?.serviceEndpoint) throw new Error("The Verifier Has No PDS Endpoint.");
  const endpoint = new URL(service.serviceEndpoint);
  if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error("The Verifier Has an Invalid PDS Endpoint.");
  const records: CustomVerification[] = [], seen = new Set<string>();
  let cursor: string | undefined;
  do {
    const url = new URL("/xrpc/com.atproto.repo.listRecords", endpoint);
    url.search = new URLSearchParams({ repo: issuer, collection: "app.bsky.graph.verification", limit: "100", ...(cursor ? { cursor } : {}) }).toString();
    const page = await publicJSON(url.href, signal) as { records?: { uri: string; value: unknown }[]; cursor?: string };
    if (!Array.isArray(page.records)) throw new Error("The Verifier Returned Invalid Records.");
    for (const record of page.records) {
      if (!record.uri.startsWith(`at://${issuer}/app.bsky.graph.verification/`)) throw new Error("The Verifier Returned Another Account's Records.");
      const value = AppBskyGraphVerification.validateRecord(record.value);
      if (value.success) records.push({ issuer, uri: record.uri, subject: value.value.subject, handle: value.value.handle, displayName: value.value.displayName });
    }
    cursor = page.cursor;
    if (cursor && seen.has(cursor)) throw new Error("The Verifier Repeated a Page Cursor.");
    if (cursor) seen.add(cursor);
  } while (cursor);
  return records;
}
