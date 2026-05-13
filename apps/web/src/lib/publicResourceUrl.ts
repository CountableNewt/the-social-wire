/**
 * URLs exposed to the browser (img, iframe, anchors) must avoid mixed content and
 * known-broken query payloads from federation tooling.
 */

/**
 * Bridgy Fed advertises PDS endpoints on `atproto.brid.gy` and `*.brid.gy`. Those relays
 * often accept `com.atproto.repo.listRecords` but return **HTTP 400** for
 * `com.atproto.sync.getBlob` from browser `<img>` loads (JSON error body despite CORS).
 *
 * **Not a substitute:** Bluesky’s image CDN (`cdn.bsky.app` in this repo’s mocks only)
 * serves App-View-resolved avatar/post assets — it is not a general drop‑in for arbitrary
 * `did`+`cid` blobs on third-party PDSes; do not fabricate CDN URLs here.
 */
export function isBridgyAtprotoPdsHostname(hostname: string): boolean {
  const h = hostname.toLowerCase();
  return h === "atproto.brid.gy" || h.endsWith(".brid.gy");
}

/** True when `rawUrl` parses and its host is a Bridgy ATProto PDS / relay endpoint. */
export function isBridgyAtprotoPdsEndpoint(rawUrl: string): boolean {
  try {
    return isBridgyAtprotoPdsHostname(new URL(rawUrl).hostname);
  } catch {
    return false;
  }
}

/**
 * Whether `href` is a `com.atproto.sync.getBlob` URL on a Bridgy relay host (unreliable for
 * hot-linked images).
 */
export function isBridgySyncGetBlobUrl(href: string): boolean {
  try {
    const u = new URL(href);
    if (!isBridgyAtprotoPdsHostname(u.hostname)) return false;
    return u.pathname.includes("com.atproto.sync.getBlob");
  } catch {
    return false;
  }
}

/** Bridgy Fed adds `bridge_*` query noise (including `bridge_completed`) that breaks many static/permalinks. */
function stripBridgyFedQueryNoise(u: URL): void {
  for (const key of [...u.searchParams.keys()]) {
    if (key.toLowerCase().startsWith("bridge_")) {
      u.searchParams.delete(key);
    }
  }
}

function finalizeHttpsPublicUrl(u: URL): string {
  if (u.protocol === "http:") {
    u.protocol = "https:";
  }
  stripBridgyFedQueryNoise(u);
  // `new URL("https://host")` uses pathname `/`; `host` + `/xrpc` must not become `host//xrpc`.
  if (u.pathname === "/" && u.search === "" && u.hash === "") {
    return u.origin;
  }
  return u.href;
}

/**
 * - Promotes `http:` to `https:` (PLC / Bridgy Fed often document `http://` PDS hosts).
 * - Drops query keys whose lowercase name starts with `bridge_` (Bridgy Fed / federation tooling),
 *   including `bridge_completed`, case-insensitive for spelling variants.
 */
export function normalizeHttpUrlToHttps(raw: string): string {
  const s = raw.trim();
  if (!s) return s;

  const parseAndFinalize = (input: string): string | undefined => {
    try {
      if (typeof URL.canParse === "function" && !URL.canParse(input)) {
        return undefined;
      }
      return finalizeHttpsPublicUrl(new URL(input));
    } catch {
      return undefined;
    }
  };

  const ok = parseAndFinalize(s);
  if (ok !== undefined) return ok;

  if (/^http:\/\//i.test(s)) {
    const promoted = `https://${s.slice("http://".length)}`;
    const okPromoted = parseAndFinalize(promoted);
    if (okPromoted !== undefined) return okPromoted;
    return promoted;
  }

  return s;
}

/**
 * Embed targets (iframes) must use the same public URL hygiene as anchors — explicit hook for callers.
 */
export function sanitizeEmbedUrlForIframe(raw: string): string {
  return normalizeHttpUrlToHttps(raw);
}

/**
 * Builds ordered `<img src>` candidates for entry rows: every value is HTTPS-normalized.
 * Never falls back from `https:` to `http:` — that triggers mixed-content warnings on HTTPS
 * app origins and does not help ATProto blob URLs.
 *
 * **Bridgy relay:** `*.brid.gy` / `atproto.brid.gy` `sync.getBlob` URLs are omitted whenever
 * any other candidate exists, and omitted entirely when alone — avoids predictable HTTP 400s
 * and useless network chatter (no HEAD; the browser still GETs the first `src`).
 */
export function thumbnailImageSrcAttempts(
  primary?: string,
  fallback?: string
): string[] {
  const normalized: string[] = [];
  for (const raw of [primary, fallback]) {
    if (!raw?.trim()) continue;
    const n = normalizeHttpUrlToHttps(raw.trim());
    if (!normalized.includes(n)) normalized.push(n);
  }
  const withoutBridgyBlob = normalized.filter((u) => !isBridgySyncGetBlobUrl(u));
  if (withoutBridgyBlob.length > 0) return withoutBridgyBlob;
  return [];
}
