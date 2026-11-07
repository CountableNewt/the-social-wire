import type { ArticlePublication } from "./articlePublishingTypes";

function siteKey(value: unknown): string | undefined {
  if (typeof value !== "string" || !value.trim()) return;
  try {
    const url = new URL(value);
    if (url.protocol === "http:" || url.protocol === "https:") return url.href.replace(/\/$/, "");
  } catch { /* AT URIs are compared without URL normalization. */ }
  return value.trim();
}

export function groupArticlesByPublication<T>(items: T[], publications: ArticlePublication[], publicationId: (item: T) => unknown): { id: string; name: string; items: T[] }[] {
  const lookup = new Map<string, ArticlePublication>();
  for (const publication of publications) {
    lookup.set(publication.uri, publication);
    const url = siteKey(publication.url);
    if (url) lookup.set(url, publication);
  }
  const groups = new Map<string, { id: string; name: string; items: T[] }>();
  for (const item of items) {
    const key = siteKey(publicationId(item));
    const publication = key ? lookup.get(key) : undefined;
    const id = publication?.uri ?? (key ? "unknown" : "unassigned");
    let group = groups.get(id);
    if (!group) {
      group = { id, name: publication?.name || (key ? "Other Publications" : "No Publication Selected"), items: [] };
      groups.set(id, group);
    }
    group.items.push(item);
  }
  return [...groups.values()].sort((a, b) => {
    const fallback = (id: string) => id === "unknown" || id === "unassigned" ? 1 : 0;
    return fallback(a.id) - fallback(b.id) || a.name.localeCompare(b.name);
  });
}
