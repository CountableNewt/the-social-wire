const STANDARD_SITE_ENTRY_COLLECTIONS = new Set([
  "site.standard.document",
  "site.standard.entry",
]);

function standardSiteEntryAtUri(uri: string | undefined): string | null {
  const trimmed = uri?.trim();
  if (!trimmed) return null;
  const m = trimmed.match(
    /^at:\/\/([^/\s"'<>]+)\/([^/\s"'<>]+)\/([^/\s"'<>?#]+)$/i
  );
  if (!m) return null;
  const collection = m[2];
  if (!STANDARD_SITE_ENTRY_COLLECTIONS.has(collection)) return null;
  return `at://${m[1]}/${collection}/${m[3]}`;
}

/**
 * Standard.site fallback content must be verified against the publisher page's
 * head AT URI. Non-standard entries, including Skyreader RSS `rssentry:` IDs,
 * do not have publisher AT URI metadata and may use their saved body fallback.
 */
export function articleFallbackContentIsVerified(args: {
  expectedAtUri?: string;
  pageAtUri?: string;
}): boolean {
  const expected = standardSiteEntryAtUri(args.expectedAtUri);
  if (!expected) return true;
  return expected === standardSiteEntryAtUri(args.pageAtUri);
}
