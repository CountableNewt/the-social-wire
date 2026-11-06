import type { ArticlePublication, PublishedArticle } from "./articlePublishingTypes";

export function publishedArticleUrl(article: PublishedArticle, publications: ArticlePublication[]): string | undefined {
  const publication = publications.find(value => value.uri === article.record.site);
  const source = typeof article.record.url === "string" ? article.record.url : typeof article.record.path === "string" && publication ? article.record.path : undefined;
  if (!source) return;
  try {
    const url = new URL(source, publication ? `${publication.url}/` : undefined);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) return;
    return url.href;
  } catch { return; }
}
