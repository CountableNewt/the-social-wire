"use client";

import { isPostSchedulingEnabled } from "@/lib/postScheduling";

import { useState, type ReactNode } from "react";
import { Pencil, Trash2 } from "lucide-react";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";
import { socialErrorMessage } from "@/lib/blueskySocialClient";
import { groupArticlesByPublication } from "@/lib/articles/articleLibraryPresentation";
import { ArticleLibraryThumbnail } from "./ArticleLibraryThumbnail";
import { ArticlePublicationGroups } from "./ArticlePublicationGroups";
import { ArticleLibraryToolbarContext } from "./ArticleLibraryToolbarContext";
import { Button, buttonVariants } from "@/components/ui/button";
import { articleEditCapability, canDeletePublishedArticle } from "@/lib/articles/articleEditCapability";

export type ArticleLibraryTab = "drafts" | "scheduled" | "published" | "research";

export function ArticleLibrary({ did, publications, publicationsReady, tab, onTabChange, drafts, loading, busy, onOpenDraft, onDeleteDraft, published, publishedPending, publishedError, onOpenPublished, onEditPublished, onDeletePublished, scheduled, research }: {
  did: string;
  publications: ArticlePublication[];
  publicationsReady: boolean;
  tab: ArticleLibraryTab;
  onTabChange: (tab: ArticleLibraryTab) => void;
  drafts: ArticleDraft[];
  loading: boolean;
  busy: boolean;
  onOpenDraft: (id: string) => void;
  onDeleteDraft: (draft: ArticleDraft) => void;
  published: PublishedArticle[];
  publishedPending: boolean;
  publishedError: unknown;
  onOpenPublished: (article: PublishedArticle) => void;
  onEditPublished: (article: PublishedArticle) => void;
  onDeletePublished: (article: PublishedArticle) => void;
  scheduled: ReactNode;
  research: ReactNode;
}) {
  const selectedTab = tab === "scheduled" && !isPostSchedulingEnabled() ? "drafts" : tab;
  const publishedRecords = new Map(published.filter(article => article.record.$type === "site.standard.document").map(article => [article.uri, article]));
  const visibleDrafts = drafts.filter(draft => !draft.publishedUri && !(draft.publishingRecordKey && publishedRecords.get(`at://${did}/site.standard.document/${draft.publishingRecordKey}`)?.record.site === draft.publicationUri));
  const [toolbar, setToolbar] = useState<HTMLElement | null>(null);
  return <section aria-label="Article Library" className="min-h-0 flex-1 overflow-y-auto p-4 sm:p-6">
    <div className="mb-6 flex w-full flex-wrap items-center gap-2 border-b pb-3"><nav aria-label="Article Sections" className="flex flex-wrap gap-2">
      {(["research", "drafts", "scheduled", "published"] as const).filter(value => value !== "scheduled" || isPostSchedulingEnabled()).map(value => <button type="button" key={value} className={buttonVariants({ variant: selectedTab === value ? "secondary" : "ghost" })} aria-pressed={selectedTab === value} disabled={busy} onClick={() => onTabChange(value)}>{value === "drafts" ? "Drafts" : value === "scheduled" ? "Scheduled" : value === "published" ? "Published" : "Research"}</button>)}
    </nav><div ref={setToolbar} className="ml-auto flex items-center" /></div>
    <ArticleLibraryToolbarContext.Provider value={toolbar}>
    {selectedTab === "research" ? research : selectedTab === "scheduled" ? scheduled : selectedTab === "drafts" ? <>
      <p className="mb-4 text-sm text-muted-foreground">Private Drafts</p>
      {loading ? <p role="status">Loading Drafts…</p> : !visibleDrafts.length ? <p className="py-12 text-center text-muted-foreground">Create an article to start writing.</p> : <ArticlePublicationGroups did={did} publications={publications} groups={groupArticlesByPublication(visibleDrafts, publications, draft => draft.publicationUri)} renderItems={items => <div className="divide-y">{items.map(draft => <div key={draft.id} className="flex items-start gap-2 rounded-lg hover:bg-muted/50 focus-within:bg-muted/50"><button type="button" disabled={busy} onClick={() => onOpenDraft(draft.id)} className="flex min-w-0 flex-1 items-start gap-4 rounded-lg px-3 py-5 text-left focus-visible:outline-ring disabled:opacity-50">
        <ArticleLibraryThumbnail did={did} draft={draft} /><span className="flex min-w-0 flex-1 flex-col gap-2">
        <span className="break-words text-lg font-semibold">{draft.title || "Untitled Article"}</span>
        {draft.excerpt ? <span className="line-clamp-2 text-sm text-muted-foreground">{draft.excerpt}</span> : null}
        <span className="text-xs text-muted-foreground">{draft.scheduleRecordKey ? "Scheduling Requested" : draft.publishingRecordKey ? "Publishing Requested" : "Draft"} · Updated <time dateTime={draft.updatedAt}>{new Date(draft.updatedAt).toLocaleString()}</time></span></span>
      </button><div className="flex shrink-0 py-5 pr-3"><Button type="button" variant="ghost" size="icon-sm" className="text-destructive hover:bg-destructive/10 hover:text-destructive" aria-label={`Delete ${draft.title || "Untitled Article"}`} disabled={busy || !!draft.scheduleRecordKey || !!draft.publishingRecordKey} onClick={() => onDeleteDraft(draft)}><Trash2 className="size-4" /></Button></div></div>)}</div>} />}
    </> : publishedPending ? <p role="status">Loading Published Articles…</p> : publishedError ? <p role="alert">{socialErrorMessage(publishedError)}</p> : !published.length ? <p className="py-12 text-center text-muted-foreground">No Published Articles Yet.</p> : <ArticlePublicationGroups did={did} publications={publications} groups={groupArticlesByPublication(published, publications, article => article.record.site)} renderItems={items => <div className="divide-y">{items.map(article => {
      const publication = publications.find(value => value.uri === article.record.site);
      const title = String(article.record.title ?? "Untitled Article");
      const editable = publicationsReady && articleEditCapability(article, publication) !== "none";
      const deletable = publicationsReady && canDeletePublishedArticle(publication);
      return <div key={article.uri} className="flex items-start gap-2 rounded-lg hover:bg-muted/50 focus-within:bg-muted/50"><button type="button" disabled={busy} onClick={() => onOpenPublished(article)} className="flex min-w-0 flex-1 items-start gap-4 rounded-lg px-3 py-5 text-left focus-visible:outline-ring">
      <ArticleLibraryThumbnail did={did} article={article} /><span className="flex min-w-0 flex-1 flex-col gap-2">
      <span className="break-words text-lg font-semibold">{String(article.record.title ?? "Untitled Article")}</span>
      {typeof article.record.description === "string" ? <span className="line-clamp-2 text-sm text-muted-foreground">{article.record.description}</span> : null}
      {typeof article.record.publishedAt === "string" ? <time dateTime={article.record.publishedAt} className="text-xs text-muted-foreground">{new Date(article.record.publishedAt).toLocaleString()}</time> : null}</span>
    </button>{editable || deletable ? <div className="flex shrink-0 gap-0.5 py-5 pr-3">{editable ? <Button type="button" variant="ghost" size="icon-sm" aria-label={`Edit ${title}`} disabled={busy} onClick={() => onEditPublished(article)}><Pencil className="size-4" /></Button> : null}{deletable ? <Button type="button" variant="ghost" size="icon-sm" className="text-destructive hover:bg-destructive/10 hover:text-destructive" aria-label={`Delete ${title}`} disabled={busy} onClick={() => onDeletePublished(article)}><Trash2 className="size-4" /></Button> : null}</div> : null}</div>;
    })}</div>} />}
    </ArticleLibraryToolbarContext.Provider>
  </section>;
}
