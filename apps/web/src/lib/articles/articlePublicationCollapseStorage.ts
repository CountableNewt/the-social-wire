export const ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY = "the-social-wire.article-collapsed-publications.v1";

function readAll(storage: Pick<Storage, "getItem">): Record<string, unknown> {
  try {
    const parsed: unknown = JSON.parse(storage.getItem(ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY) ?? "{}");
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

export function loadCollapsedArticlePublications(storage: Pick<Storage, "getItem">, did: string): string[] {
  const value = readAll(storage)[did];
  return Array.isArray(value) ? value.filter((id): id is string => typeof id === "string") : [];
}

export function saveCollapsedArticlePublications(storage: Pick<Storage, "getItem" | "setItem">, did: string, ids: string[]): void {
  try {
    const all = readAll(storage);
    if (ids.length) all[did] = ids;
    else delete all[did];
    storage.setItem(ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY, JSON.stringify(all));
  } catch {
    /* quota / private mode */
  }
}
