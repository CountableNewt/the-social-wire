/**
 * URL normalization + deterministic repo keys aligned with upstream L@tr (latr-link).
 */

import { normalizeHttpUrlToHttps } from "@/lib/publicResourceUrl";

const TRACKING_PARAMS = new Set([
  "utm_source",
  "utm_medium",
  "utm_campaign",
  "utm_term",
  "utm_content",
  "fbclid",
  "gclid",
  "ref",
]);

function stripTracking(searchParams: URLSearchParams): void {
  const toDelete: string[] = [];
  for (const key of searchParams.keys()) {
    const lower = key.toLowerCase();
    if (lower.startsWith("utm_") || TRACKING_PARAMS.has(lower)) {
      toDelete.push(key);
    }
  }
  for (const k of toDelete) {
    searchParams.delete(k);
  }
}

/**
 * Returns a canonical normalized URL string or `null` if input is not http(s).
 */
export function normalizeLatrHttpsUrl(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const promoted = normalizeHttpUrlToHttps(trimmed);
  if (!promoted.trim()) return null;

  let url: URL;
  try {
    url = new URL(promoted);
  } catch {
    return null;
  }

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return null;
  }

  url.protocol = url.protocol.toLowerCase();
  url.hostname = url.hostname.toLowerCase();
  url.hash = "";

  stripTracking(url.searchParams);
  url.searchParams.sort();

  if (url.pathname !== "/" && url.pathname.endsWith("/")) {
    url.pathname = url.pathname.replace(/\/+$/, "");
  }

  return url.toString();
}

const B32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

/** RFC 4648 base32 (no padding), uppercase alphabet. */
export function bytesToBase32Upper(buf: Uint8Array): string {
  let bits = 0;
  let value = 0;
  let out = "";
  for (let i = 0; i < buf.length; i++) {
    value = (value << 8) | buf[i];
    bits += 8;
    while (bits >= 5) {
      out += B32[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) {
    out += B32[(value << (5 - bits)) & 31];
  }
  return out;
}

export async function sha256Utf8(text: string): Promise<Uint8Array> {
  const data = new TextEncoder().encode(text);
  const buf = await crypto.subtle.digest("SHA-256", data);
  return new Uint8Array(buf);
}

export async function latrExternalRkeyFromNormalizedUrl(
  normalizedUrl: string
): Promise<string> {
  const hash = await sha256Utf8(normalizedUrl);
  return bytesToBase32Upper(hash);
}

export async function latrItemRkeyFromSubjectUri(subjectUri: string): Promise<string> {
  const hash = await sha256Utf8(subjectUri);
  return bytesToBase32Upper(hash);
}

export function latrFingerprintHex(buf: Uint8Array): string {
  return [...buf].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export async function latrFingerprintFromNormalizedUrl(
  normalizedUrl: string
): Promise<string> {
  const hash = await sha256Utf8(normalizedUrl);
  return latrFingerprintHex(hash);
}
