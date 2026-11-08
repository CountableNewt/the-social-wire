import { articlePublicationDisabledReason } from "./articlePublicationCapabilities";
import type { ArticlePublication, ArticleRecord, PublishedArticle } from "./articlePublishingTypes";

/** "full" edits the Markdown body; "details" keeps a native body and edits only its metadata. */
export type ArticleEditCapability = "full" | "details" | "none";

/** Inline Markdown only: a record whose text moved to a blob keeps just a truncated preview inline. */
export function publishedArticleMarkdown(record: ArticleRecord): string | undefined {
  const content = record.content as { $type?: unknown; text?: { markdown?: unknown; textBlob?: unknown } } | undefined;
  if (content?.$type !== "at.markpub.markdown" || content.text?.textBlob) return undefined;
  return typeof content.text?.markdown === "string" ? content.text.markdown : undefined;
}

export function articleEditCapability(article: PublishedArticle, publication: ArticlePublication | undefined): ArticleEditCapability {
  if (!publication || articlePublicationDisabledReason(publication)) return "none";
  const markdownHost = publication.host === "markpub" || publication.host === "unknown";
  return markdownHost && publishedArticleMarkdown(article.record) !== undefined ? "full" : "details";
}

/** A missing publication does not block deleting an orphaned document. */
export function canDeletePublishedArticle(publication: ArticlePublication | undefined): boolean {
  return !publication || !articlePublicationDisabledReason(publication);
}
