// Aggregate navigation without sending query state, OAuth parameters, or a
// publication identity embedded in a reader URL to the analytics provider.
export function vercelAnalyticsUrl(value: string): string | null {
  try {
    const url = new URL(value);
    if (url.protocol !== "https:" && url.protocol !== "http:") return null;
    url.search = "";
    url.hash = "";
    if (url.pathname.startsWith("/read/")) url.pathname = "/read/[publication]";
    return url.toString();
  } catch {
    return null;
  }
}
