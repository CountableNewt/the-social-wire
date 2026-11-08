import { parseAtUri } from "@/lib/atprotoClient";
import type { MergedLatrSave } from "@/lib/pdsClient";
import { isOriginalEntryContentUri } from "@/lib/savedLinkSocialTarget";

export type SavedLinkCategory = "articles" | "social" | "other";

export const SAVED_LINK_CATEGORIES: readonly SavedLinkCategory[] = [
  "articles",
  "social",
  "other",
];

const SOCIAL_POST_COLLECTIONS = new Set(["app.bsky.feed.post"]);

/** Host → path pattern identifying an individual social post (not a profile or homepage). */
const SOCIAL_POST_URL_PATTERNS: ReadonlyArray<[string, RegExp]> = [
  ["bsky.app", /^\/profile\/[^/]+\/post\/[^/]+/],
  ["x.com", /^\/[^/]+\/status\/\d+/],
  ["twitter.com", /^\/[^/]+\/status\/\d+/],
  ["threads.net", /^\/@[^/]+\/post\/[^/]+/],
  ["threads.com", /^\/@[^/]+\/post\/[^/]+/],
  ["mastodon.social", /^\/@[^/]+\/\d+/],
  ["reddit.com", /^\/r\/[^/]+\/comments\/[^/]+/],
  ["linkedin.com", /^\/(posts|feed\/update)\//],
];

function hostMatches(hostname: string, host: string): boolean {
  return hostname === host || hostname.endsWith(`.${host}`);
}

function isSocialPostUrl(raw: string | undefined): boolean {
  if (!raw) return false;
  let url: URL;
  try {
    url = new URL(raw.trim());
  } catch {
    return false;
  }
  const hostname = url.hostname.toLowerCase();
  return SOCIAL_POST_URL_PATTERNS.some(
    ([host, pattern]) => hostMatches(hostname, host) && pattern.test(url.pathname),
  );
}

export function savedLinkCategory(row: MergedLatrSave): SavedLinkCategory {
  if (row.kind === "native") {
    const collection = parseAtUri(row.subjectUri)?.collection;
    if (collection && SOCIAL_POST_COLLECTIONS.has(collection)) return "social";
    if (isOriginalEntryContentUri(row.subjectUri)) return "articles";
  }
  if (isSocialPostUrl(row.linkedWebUrl) || isSocialPostUrl(row.url)) {
    return "social";
  }
  if (row.title?.trim() && row.excerpt?.trim()) return "articles";
  return "other";
}

export function parseSavedLinkCategory(
  value: string | null | undefined,
): SavedLinkCategory | null {
  return SAVED_LINK_CATEGORIES.find((category) => category === value) ?? null;
}

export function filterSavedLinksByCategory(
  rows: MergedLatrSave[],
  category: SavedLinkCategory | null,
): MergedLatrSave[] {
  if (!category) return rows;
  return rows.filter((row) => savedLinkCategory(row) === category);
}

export function countSavedLinkCategories(
  rows: MergedLatrSave[],
): Record<SavedLinkCategory, number> {
  const counts: Record<SavedLinkCategory, number> = {
    articles: 0,
    social: 0,
    other: 0,
  };
  for (const row of rows) counts[savedLinkCategory(row)] += 1;
  return counts;
}
