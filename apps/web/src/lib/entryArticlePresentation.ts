/** RSS summaries and short excerpts should not block live-site iframe reading. */
export function isSubstantialArticleBody(html: string): boolean {
  const trimmed = html.trim();
  if (!trimmed) return false;
  if (trimmed.length >= 600) return true;
  const textOnly = trimmed
    .replace(/<[^>]+>/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return textOnly.length >= 280;
}

export type EntryArticlePresentation = "embed" | "html";

export function resolveEntryArticlePresentation(args: {
  contentHtml: string;
  embedUrl?: string;
  originalUrl?: string;
}): EntryArticlePresentation | null {
  const substantial = isSubstantialArticleBody(args.contentHtml);
  if (substantial) return "html";
  const embedTarget = args.embedUrl?.trim() || args.originalUrl?.trim();
  if (embedTarget) return "embed";
  if (args.contentHtml.trim()) return "html";
  return null;
}
