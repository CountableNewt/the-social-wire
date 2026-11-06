"use client";

import { useMemo } from "react";
import { BlockDocumentRenderer, importMarkdownDocument } from "@stygian/markdown-editor";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { publishedArticleUrl } from "@/lib/articles/publishedArticleUrl";
import { EntryArticleEmbed } from "@/components/EntryDetail/EntryArticleEmbed";
import { floatingGlassClasses } from "@/components/shared/floatingChromeStyles";
import { ArticlePanelResizeHandle } from "./ArticlePanelResizeHandle";

export function PublishedArticleView({ article, publications, metadataVisible, metadataWidth, onMetadataWidthChange }: {
  article: PublishedArticle;
  publications: ArticlePublication[];
  metadataVisible: boolean;
  metadataWidth: number;
  onMetadataWidthChange: (width: number) => void;
}) {
  const { record } = article;
  const title = typeof record.title === "string" ? record.title : "Untitled Article";
  const content = record.content as { $type?: string; text?: { markdown?: unknown } } | undefined;
  const markdown = content?.$type === "at.markpub.markdown" && typeof content.text?.markdown === "string" ? content.text.markdown : undefined;
  const document = useMemo(() => markdown ? importMarkdownDocument(markdown) : undefined, [markdown]);
  const url = publishedArticleUrl(article, publications);
  const publication = publications.find(value => value.uri === record.site);
  const plainBody = typeof record.textContent === "string" ? record.textContent : "";
  return <div className="flex min-h-0 flex-1 flex-col overflow-y-auto lg:flex-row lg:overflow-hidden">
    <section aria-label="Published Article" className="min-h-0 min-w-0 flex-1 p-4 sm:p-6 lg:overflow-y-auto">
      <header className="mb-6 space-y-3"><h1 className="break-words text-3xl font-semibold">{title}</h1>{typeof record.description === "string" ? <p className="text-muted-foreground">{record.description}</p> : null}<p className="text-xs text-muted-foreground">Published Article</p></header>
      {document ? <article className="article-content"><BlockDocumentRenderer document={document} resolveImageURL={value => /^https?:\/\//i.test(value) ? value : undefined} /></article> : url ? <div className="flex h-[70svh] min-h-96 flex-col overflow-hidden"><EntryArticleEmbed url={url} title={title} expectedAtUri={article.uri} fallbackContent={<p className="whitespace-pre-wrap">{plainBody}</p>} /></div> : <article className="article-content whitespace-pre-wrap">{plainBody || "Open this article in its publishing app to view its content."}</article>}
    </section>
    <aside id="article-metadata" aria-label="Article Details" hidden={!metadataVisible} style={{ "--article-metadata-width": `${metadataWidth}px` } as React.CSSProperties} className={`${floatingGlassClasses} relative m-4 min-h-0 shrink-0 p-4 lg:w-[var(--article-metadata-width)] lg:max-w-[40%] ${metadataVisible ? "" : "hidden"}`}>
      <ArticlePanelResizeHandle width={metadataWidth} onWidthChange={onMetadataWidthChange} />
      <div className="h-full space-y-4 overflow-y-auto"><h2 className="text-sm font-semibold">Article Details</h2>
        {publication ? <p className="text-sm">{publication.name}</p> : null}
        {typeof record.publishedAt === "string" ? <p className="text-sm"><time dateTime={record.publishedAt}>{new Date(record.publishedAt).toLocaleString()}</time></p> : null}
        {Array.isArray(record.tags) ? <p className="break-words text-sm">{record.tags.filter(value => typeof value === "string").join(", ")}</p> : null}
        {url ? <a href={url} target="_blank" rel="noopener noreferrer" className="text-sm text-primary hover:underline">Open Article</a> : null}
        <p className="break-all text-xs text-muted-foreground">{article.uri}</p>
      </div>
    </aside>
  </div>;
}
