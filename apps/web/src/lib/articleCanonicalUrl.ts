import type { EntryDetail } from "@/lib/atprotoClient";
import { normalizeHttpUrlToHttps } from "@/lib/publicResourceUrl";

/**
 * HTTPS-stable canonical browser URL for an entry (embedded page or original permalink).
 */
export function canonicalArticleHttpsUrl(entry: EntryDetail): string | null {
  const raw =
    entry.embedUrl ??
    entry.originalUrl ??
    (typeof window !== "undefined" ? window.location.href : "");
  if (raw.startsWith("http://") || raw.startsWith("https://")) {
    return normalizeHttpUrlToHttps(raw);
  }
  return null;
}
