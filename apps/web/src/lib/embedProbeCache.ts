/** In-memory cache of `/api/embed-frame` probe results keyed by sanitized iframe src. */
const embedProbeCache = new Map<string, boolean>();

export function getCachedEmbedProbeFrameable(iframeSrc: string): boolean | undefined {
  return embedProbeCache.get(iframeSrc);
}

export function setCachedEmbedProbeFrameable(
  iframeSrc: string,
  frameable: boolean
): void {
  embedProbeCache.set(iframeSrc, frameable);
}
