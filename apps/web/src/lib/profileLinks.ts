import { jsonToLex, RichText } from "@atproto/api";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";

const PROFILE = "app.bsky.actor.profile";
const LINK = "app.bsky.actor.link";

export type ProfileLinkCard = {
  uri: string;
  url: string;
  title?: string;
  domain: string;
  iconUrl?: string;
};

type JSONRecord = Record<string, unknown>;
function object(value: unknown): value is JSONRecord { return !!value && typeof value === "object" && !Array.isArray(value); }
function validCID(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 256) return false;
  try { return String(jsonToLex({ $link: value })) === value; } catch { return false; }
}
function httpsURL(value: unknown): URL | undefined {
  if (typeof value !== "string") return;
  try { const url = new URL(value); if (url.protocol === "https:" && !url.username && !url.password) return url; } catch { /* Invalid public URL. */ }
}
function abort(error: unknown, signal?: AbortSignal): void {
  signal?.throwIfAborted();
  if (error instanceof Error && error.name === "AbortError") throw error;
}
class PublicProfileRequestError extends Error {
  constructor(readonly status: number, readonly code?: string) { super(`Profile links request failed (${status}).`); }
}
async function publicJSON(url: string, signal?: AbortSignal): Promise<unknown> {
  signal?.throwIfAborted();
  const response = await fetch(url, { credentials: "omit", redirect: "error", headers: { Accept: "application/json" }, signal });
  signal?.throwIfAborted();
  const data: unknown = await response.json();
  signal?.throwIfAborted();
  if (!response.ok) throw new PublicProfileRequestError(response.status, object(data) && typeof data.error === "string" ? data.error : undefined);
  return data;
}

async function profilePDS(did: string, signal?: AbortSignal): Promise<URL> {
  const network = getAtprotoNetwork();
  const isolated = network.plcDirectory !== "https://plc.directory" || network.publicAppView !== "https://public.api.bsky.app" || network.appViewDid !== "did:web:api.bsky.app";
  let didURL: string;
  if (/^did:plc:[a-z2-7]{24}$/.test(did)) didURL = `${network.plcDirectory}/${encodeURIComponent(did)}`;
  else if (/^did:web:[^\s/?#\\]+$/.test(did)) {
    if (isolated) throw new Error("DID-web profile links are unavailable in the isolated local network.");
    const [encodedHost, ...encodedPath] = did.slice(8).split(":");
    const host = decodeURIComponent(encodedHost);
    const origin = httpsURL(`https://${host}`);
    if (!origin || origin.pathname !== "/" || origin.search || origin.hash) throw new Error("The profile has an invalid DID-web host.");
    const segments = encodedPath.map(segment => {
      const decoded = decodeURIComponent(segment);
      if (!decoded || decoded === "." || decoded === ".." || /[/\\]/.test(decoded)) throw new Error("The profile has an invalid DID-web path.");
      return encodeURIComponent(decoded);
    });
    didURL = `${origin.origin}/${segments.length ? `${segments.join("/")}/did.json` : ".well-known/did.json"}`;
  } else throw new Error("The profile has an unsupported DID.");
  const doc = await publicJSON(didURL, signal);
  if (!object(doc) || doc.id !== did || !Array.isArray(doc.service)) throw new Error("The profile DID document does not match its identity.");
  const service = doc.service.find(value => object(value) && (value.id === "#atproto_pds" || value.id === `${did}#atproto_pds`) && value.type === "AtprotoPersonalDataServer");
  const endpoint = object(service) ? httpsURL(service.serviceEndpoint) : undefined;
  if (!endpoint || endpoint.search || endpoint.hash) throw new Error("The profile has an invalid PDS endpoint.");
  if (isolated) {
    const allowed = (process.env.NEXT_PUBLIC_LOCAL_ATPROTO_PROFILE_PDS_URLS ?? "").split(",").map(value => value.trim()).filter(Boolean).map(value => {
      const url = httpsURL(value);
      if (!url || url.pathname !== "/" || url.search || url.hash) throw new Error("Local profile PDS allowlist must contain HTTPS origins.");
      return url.origin;
    });
    if (!allowed.includes(endpoint.origin)) throw new Error("The profile PDS is not allowed in the isolated local network.");
  }
  return endpoint;
}

function recordURL(pds: URL, did: string, collection: string, rkey: string, cid?: string): string {
  const url = new URL("/xrpc/com.atproto.repo.getRecord", pds);
  url.search = new URLSearchParams({ repo: did, collection, rkey, ...(cid ? { cid } : {}) }).toString();
  return url.href;
}
function parseReference(value: unknown, did: string): { uri: string; cid: string; rkey: string } | undefined {
  if (!object(value) || typeof value.uri !== "string" || !validCID(value.cid)) return;
  const prefix = `at://${did}/${LINK}/`;
  if (!value.uri.startsWith(prefix)) return;
  const rkey = value.uri.slice(prefix.length);
  if (!/^[A-Za-z0-9._~:-]{1,512}$/.test(rkey) || rkey === "." || rkey === "..") return;
  return { uri: value.uri, cid: value.cid, rkey };
}
function cardFromRecord(value: unknown, pds: URL, did: string, uri: string): ProfileLinkCard | undefined {
  if (!object(value) || value.$type !== LINK || typeof value.createdAt !== "string" || !/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(value.createdAt) || !Number.isFinite(Date.parse(value.createdAt))) return;
  const url = httpsURL(value.url);
  if (!url) return;
  if (value.title !== undefined && (typeof value.title !== "string" || new RichText({ text: value.title }).graphemeLength > 40 || new TextEncoder().encode(value.title).length > 320)) return;
  let iconUrl: string | undefined;
  if (value.icon !== undefined) {
    const icon = value.icon;
    if (!object(icon) || icon.$type !== "blob" || !object(icon.ref) || !validCID(icon.ref.$link) || !["image/png", "image/jpeg", "image/webp"].includes(String(icon.mimeType)) || typeof icon.size !== "number" || !Number.isInteger(icon.size) || icon.size < 0 || icon.size > 100_000) return;
    const blobURL = new URL("/xrpc/com.atproto.sync.getBlob", pds);
    blobURL.search = new URLSearchParams({ did, cid: icon.ref.$link }).toString();
    iconUrl = blobURL.href;
  }
  return { uri, url: url.href, title: typeof value.title === "string" && value.title.trim() ? value.title : undefined, domain: url.hostname, iconUrl };
}

/** Only profile-pinned actor link records are shown, in the owner's declared order. */
export async function loadProfileLinks(did: string, signal?: AbortSignal): Promise<ProfileLinkCard[]> {
  signal?.throwIfAborted();
  const pds = await profilePDS(did, signal);
  let profile: unknown;
  try { profile = await publicJSON(recordURL(pds, did, PROFILE, "self"), signal); }
  catch (error) {
    abort(error, signal);
    if (error instanceof PublicProfileRequestError && error.code === "RecordNotFound") return [];
    throw error;
  }
  if (!object(profile) || profile.uri !== `at://${did}/${PROFILE}/self` || !validCID(profile.cid) || !object(profile.value) || profile.value.$type !== PROFILE) throw new Error("The PDS returned an invalid profile record.");
  if (profile.value.links === undefined) return [];
  if (!Array.isArray(profile.value.links)) throw new Error("The profile link references are invalid.");
  const seen = new Set<string>();
  const references = profile.value.links.slice(0, 10).flatMap(value => {
    const reference = parseReference(value, did);
    if (!reference || seen.has(reference.uri)) return [];
    seen.add(reference.uri);
    return [reference];
  });
  const cards = await Promise.all(references.map(async reference => {
    try {
      const record = await publicJSON(recordURL(pds, did, LINK, reference.rkey, reference.cid), signal);
      if (!object(record) || record.uri !== reference.uri || record.cid !== reference.cid) return;
      return cardFromRecord(record.value, pds, did, reference.uri);
    } catch (error) { abort(error, signal); return; }
  }));
  signal?.throwIfAborted();
  return cards.filter((card): card is ProfileLinkCard => !!card);
}
