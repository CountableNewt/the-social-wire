/** A completed article update with unavailable upstream RSS sources. */
export class PublicationSourceRefreshError extends Error {
  readonly statusMessage = "Articles Updated — Some Sources Unavailable";

  constructor(readonly hosts: readonly string[], readonly unavailablePublicationIds: readonly string[] = []) {
    super(`Articles updated. Unavailable sources: ${hosts.join(", ")}. These publishers may be temporarily unavailable or their feed may have moved.`);
    this.name = "PublicationSourceRefreshError";
  }
}

/** Accept only the server's explicit, completed RSS-only partial result. */
export function sourceRefreshErrorFromResponse(status: number, body: unknown): PublicationSourceRefreshError | undefined {
  if (status !== 503 || !body || typeof body !== "object") return;
  const value = body as { error?: unknown; message?: unknown; articlesUpdated?: unknown; sourceFailures?: unknown; unavailablePublicationIds?: unknown };
  if (value.error !== "Service Unavailable" || typeof value.message !== "string" || !value.message.trim() ||
      value.articlesUpdated !== true || !Array.isArray(value.sourceFailures) || !value.sourceFailures.length) return;
  const hosts = new Set<string>();
  for (const failure of value.sourceFailures) {
    if (!failure || typeof failure !== "object" ||
        !["rss_backoff", "rss_fetch", "rss_parse"].includes(failure.category) ||
        !["other", "network", "timeout"].includes(failure.kind) ||
        typeof failure.host !== "string" || !/^[a-z0-9.-]{1,253}$/.test(failure.host) ||
        !Number.isSafeInteger(failure.count) || failure.count <= 0 ||
        (failure.status !== undefined && (!Number.isInteger(failure.status) || failure.status < 100 || failure.status > 599))) return;
    hosts.add(failure.host);
  }
  const ids = value.unavailablePublicationIds;
  if (ids !== undefined && (!Array.isArray(ids) || ids.some(id => typeof id !== "string" || !id.startsWith("rss:") || id.length <= 4))) return;
  return new PublicationSourceRefreshError([...hosts].sort(), ids ? [...new Set(ids as string[])].sort() : []);
}
