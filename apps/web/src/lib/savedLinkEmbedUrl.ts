import type { MergedLatrSave } from "@/lib/pdsClient";
import { sanitizeEmbedUrlForIframe } from "@/lib/publicResourceUrl";

/** Pocket reader / redirect hosts that loop or auth-fail inside sandboxed iframes. */
export function isPocketReaderHostname(hostname: string): boolean {
  const h = hostname.trim().toLowerCase();
  return (
    h === "getpocket.com" ||
    h.endsWith(".getpocket.com") ||
    h === "readitlaterlist.com" ||
    h.endsWith(".pocket.com") ||
    h === "pckt.it" ||
    h === "pkt.cool"
  );
}

export function isPoorIframeEmbedTarget(rawUrl: string): boolean {
  try {
    return isPocketReaderHostname(new URL(rawUrl.trim()).hostname);
  } catch {
    return false;
  }
}

/**
 * Canonical HTTPS URL to load in the saved-link reader iframe.
 * Prefers `linkedWebUrl` when the stored wrapper is a Pocket reader redirect.
 */
export function resolveSavedLinkEmbedUrl(row: MergedLatrSave): string | undefined {
  const linked = row.linkedWebUrl?.trim();
  const primary =
    row.kind === "external" ? row.url.trim() : row.url?.trim() ?? undefined;

  if (primary && isPoorIframeEmbedTarget(primary)) {
    return linked ?? primary;
  }
  return primary ?? linked;
}

export function stableSavedLinkIframeSrc(row: MergedLatrSave): string | undefined {
  const resolved = resolveSavedLinkEmbedUrl(row);
  if (!resolved) return undefined;
  return sanitizeEmbedUrlForIframe(resolved);
}
