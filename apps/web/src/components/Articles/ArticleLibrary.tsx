"use client";

import { useState, type ReactNode } from "react";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { socialErrorMessage } from "@/lib/blueskySocialClient";
import { groupArticlesByPublication } from "@/lib/articles/articleLibraryPresentation";
import { ArticleLibraryThumbnail } from "./ArticleLibraryThumbnail";
import { ArticlePublicationGroups } from "./ArticlePublicationGroups";
import { ArticleLibraryToolbarContext } from "./ArticleLibraryToolbarContext";
import { buttonVariants } from "@/components/ui/button";

export type ArticleLibraryTab = "drafts" | "scheduled" | "published" | "research";

export function ArticleLibrary({ did, publications, tab, onTabChange, drafts, loading, busy, onOpenDraft, published, publishedPending, publishedError, onOpenPublished, scheduled, research }: {
  did: string;
  publications: ArticlePublication[];
  tab: ArticleLibraryTab;
  onTabChange: (tab: ArticleLibraryTab) => void;
  drafts: ArticleDraft[];
  loading: boolean;
  busy: boolean;
  onOpenDraft: (id: string) => void;
  published: PublishedArticle[];
  publishedPending: boolean;
  publishedError: unknown;
  onOpenPublished: (article: PublishedArticle) => void;
  scheduled: ReactNode;
  research: ReactNode;
}) {
  const [toolbar, setToolbar] = useState<HTMLElement | null>(null);
  return <section aria-label="Article Library" className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
    <div className="mb-6 flex w-full flex-wrap items-center gap-2 border-b pb-3"><nav aria-label="Article Sections" className="flex flex-wrap gap-2">
      {(["research", "drafts", "scheduled", "published"] as const).map(value => <button type="button" key={value} className={buttonVariants({ variant: tab === value ? "secondary" : "ghost" })} aria-pressed={tab === value} disabled={busy} onClick={() => onTabChange(value)}>{value === "drafts" ? "Drafts" : value === "scheduled" ? "Scheduled" : value === "published" ? "Published" : "Research"}</button>)}
    </nav><div ref={setToolbar} className="ml-auto flex items-center" /></div>
    <ArticleLibraryToolbarContext.Provider value={toolbar}>
    {tab === "research" ? research : tab === "scheduled" ? scheduled : tab === "drafts" ? <>
      <p className="mb-4 text-sm text-muted-foreground">Private Drafts</p>
      {loading ? <p role="status">Loading Drafts…</p> : !drafts.length ? <p className="py-12 text-center text-muted-foreground">Create an article to start writing.</p> : <ArticlePublicationGroups did={did} publications={publications} groups={groupArticlesByPublication(drafts, publications, draft => draft.publicationUri)} renderItems={items => <div className="divide-y">{items.map(draft => <button key={draft.id} type="button" disabled={busy} onClick={() => onOpenDraft(draft.id)} className="flex w-full items-start gap-4 rounded-lg px-3 py-5 text-left hover:bg-muted/50 focus-visible:outline-ring disabled:opacity-50">
        <ArticleLibraryThumbnail did={did} draft={draft} /><span className="flex min-w-0 flex-1 flex-col gap-2">
        <span className="break-words text-lg font-semibold">{draft.title || "Untitled Article"}</span>
        {draft.excerpt ? <span className="line-clamp-2 text-sm text-muted-foreground">{draft.excerpt}</span> : null}
        <span className="text-xs text-muted-foreground">{draft.publishedUri ? "Published" : draft.scheduleRecordKey ? "Scheduling Requested" : draft.publishingRecordKey ? "Publishing Requested" : "Draft"} · Updated <time dateTime={draft.updatedAt}>{new Date(draft.updatedAt).toLocaleString()}</time></span></span>
      </button>)}</div>} />}
    </> : publishedPending ? <p role="status">Loading Published Articles…</p> : publishedError ? <p role="alert">{socialErrorMessage(publishedError)}</p> : !published.length ? <p className="py-12 text-center text-muted-foreground">No Published Articles Yet.</p> : <ArticlePublicationGroups did={did} publications={publications} groups={groupArticlesByPublication(published, publications, article => article.record.site)} renderItems={items => <div className="divide-y">{items.map(article => <button key={article.uri} type="button" disabled={busy} onClick={() => onOpenPublished(article)} className="flex w-full items-start gap-4 rounded-lg px-3 py-5 text-left hover:bg-muted/50 focus-visible:outline-ring">
      <ArticleLibraryThumbnail did={did} article={article} /><span className="flex min-w-0 flex-1 flex-col gap-2">
      <span className="break-words text-lg font-semibold">{String(article.record.title ?? "Untitled Article")}</span>
      {typeof article.record.description === "string" ? <span className="line-clamp-2 text-sm text-muted-foreground">{article.record.description}</span> : null}
      {typeof article.record.publishedAt === "string" ? <time dateTime={article.record.publishedAt} className="text-xs text-muted-foreground">{new Date(article.record.publishedAt).toLocaleString()}</time> : null}</span>
    </button>)}</div>} />}
    </ArticleLibraryToolbarContext.Provider>
  </section>;
}
