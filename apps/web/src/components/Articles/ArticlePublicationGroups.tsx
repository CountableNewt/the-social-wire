"use client";

import { useContext, useId, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ChevronRight } from "lucide-react";
import { useCollapsedArticlePublications } from "@/hooks/articles/useCollapsedArticlePublications";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";
import { Button } from "@/components/ui/button";
import { ArticlePublicationIcon } from "./ArticlePublicationIcon";
import { ArticleLibraryToolbarContext } from "./ArticleLibraryToolbarContext";

export function ArticlePublicationGroups<T>({ did, publications, groups, renderItems, className = "space-y-8" }: {
  did: string;
  publications: ArticlePublication[];
  groups: { id: string; name: string; items: T[] }[];
  renderItems: (items: T[]) => ReactNode;
  className?: string;
}) {
  const baseId = useId().replace(/:/g, "");
  const toolbar = useContext(ArticleLibraryToolbarContext);
  const { collapsed, toggle, setAll } = useCollapsedArticlePublications(did);
  const ids = groups.map(group => group.id);
  const allCollapsed = ids.every(id => collapsed.has(id));
  const bulkToggle = <Button type="button" size="sm" variant="ghost" onClick={() => setAll(ids, !allCollapsed)}>{allCollapsed ? "Expand All" : "Collapse All"}</Button>;
  return <div className={className}>
    {groups.length > 1 ? toolbar ? createPortal(bulkToggle, toolbar) : <div className="flex justify-end">{bulkToggle}</div> : null}
    {groups.map((group, index) => {
      const expanded = !collapsed.has(group.id);
      const contentId = `${baseId}-publication-${index}`;
      return <section key={group.id} aria-label={group.name}>
        <h2 className="mb-2 text-base font-semibold"><button type="button" aria-expanded={expanded} aria-controls={contentId} onClick={() => toggle(group.id)} className="-mx-2 flex w-[calc(100%+1rem)] items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted/50 focus-visible:outline-ring">
          <ChevronRight aria-hidden="true" className={`size-4 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none ${expanded ? "rotate-90" : ""}`} />
          <ArticlePublicationIcon publication={publications.find(publication => publication.uri === group.id)} />
          <span className="min-w-0 flex-1 truncate">{group.name}</span>
          <span className="shrink-0 text-xs font-normal text-muted-foreground">{group.items.length}</span>
        </button></h2>
        <div id={contentId} hidden={!expanded}>{expanded ? renderItems(group.items) : null}</div>
      </section>;
    })}
  </div>;
}
