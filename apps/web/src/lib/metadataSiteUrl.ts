/** Canonical social-card origin; OAuth identity remains tied to its request origin. */
export function metadataSiteUrl(environment: Record<string, string | undefined> = process.env): string {
  const explicit = environment.NEXT_PUBLIC_SITE_URL?.trim();
  const deployment = environment.VERCEL_URL?.trim();
  const raw = explicit || (deployment ? `https://${deployment}` : "https://thesocialwire.app");
  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error("Metadata Requires a Valid Public Site Origin.");
  }
  if (!["http:", "https:"].includes(parsed.protocol) || parsed.username || parsed.password ||
    parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error("Metadata Requires a Valid Public Site Origin.");
  }
  return parsed.origin;
}
