/**
 * Legacy deterministic key formats used before Stygian contract parity.
 * Listing repair code uses these to locate divergent L@tr PDS records.
 */

const LEGACY_LOWER_B32 = "abcdefghijklmnopqrstuvwxyz234567";

function legacyLowerBase32Encode(buf: Uint8Array): string {
  let bits = 0;
  let value = 0;
  let out = "";
  for (let i = 0; i < buf.length; i++) {
    value = (value << 8) | buf[i];
    bits += 8;
    while (bits >= 5) {
      out += LEGACY_LOWER_B32[(value >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) {
    out += LEGACY_LOWER_B32[(value << (5 - bits)) & 31];
  }
  return out;
}

async function sha256Utf8(text: string): Promise<Uint8Array> {
  const data = new TextEncoder().encode(text);
  const buf = await crypto.subtle.digest("SHA-256", data);
  return new Uint8Array(buf);
}

/** Legacy iOS Social Wire: lowercase 52-char base32 prefix, lowercased URL for externals. */
export async function legacyIOSLatrExternalRkey(
  normalizedUrl: string
): Promise<string> {
  const hash = await sha256Utf8(normalizedUrl.toLowerCase());
  return legacyLowerBase32Encode(hash).slice(0, 52).toLowerCase();
}

/** Legacy iOS Social Wire: lowercase 52-char base32 prefix. */
export async function legacyIOSLatrItemRkey(
  subjectUri: string
): Promise<string> {
  const hash = await sha256Utf8(subjectUri);
  return legacyLowerBase32Encode(hash).slice(0, 52).toLowerCase();
}

export function isLegacyLatrExternalRkey(
  canonicalRkey: string,
  candidateRkey: string
): boolean {
  return candidateRkey === canonicalRkey.toLowerCase();
}
