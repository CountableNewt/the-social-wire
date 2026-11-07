"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { BookOpen, Highlighter, RefreshCw } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { useSembleCollections } from "@/hooks/useSembleReadLater";
import { useMarginAnnotations } from "@/hooks/research/useMarginAnnotations";
import { readLaterQueryKeys } from "@/lib/semble";
import { ResearchCollection } from "./ResearchCollection";
import { ResearchSourceLink } from "./ResearchSourceLink";

export function ResearchWorkspace({ did }: { did: string }) {
  const [source, setSource] = useState<"semble" | "margin">("semble");
  const [refreshing, setRefreshing] = useState(false);
  const [selection, setSelection] = useState<{ did: string; uri: string }>();
  const cache = useQueryClient();
  const semble = useSembleCollections({ enabled: source === "semble" });
  const margin = useMarginAnnotations(did, source === "margin");
  const refresh = async () => {
    setRefreshing(true);
    try {
      await Promise.all([
        cache.invalidateQueries({ queryKey: readLaterQueryKeys.root(did, "semble") }),
        cache.invalidateQueries({ queryKey: ["research", did, "margin"] }),
      ]);
    } finally { setRefreshing(false); }
  };
  return <section aria-label="Research Library" className="min-h-0 min-w-0 flex-1 overflow-y-auto">
    <div className="mx-auto w-full max-w-6xl space-y-6 p-4 sm:p-6">
      <header className="flex flex-wrap items-center justify-between gap-3"><div><h1 className="text-xl font-semibold">Research</h1><p className="mt-1 text-sm text-muted-foreground">Browse your Semble collections and Margin annotations for your next article.</p></div><Button variant="outline" size="sm" disabled={refreshing} onClick={() => { void refresh(); }}><RefreshCw className="size-4" />{refreshing ? "Refreshing…" : "Refresh Research"}</Button></header>
      <nav aria-label="Research Sources" className="flex flex-wrap gap-2"><button type="button" className={buttonVariants({ variant: source === "semble" ? "secondary" : "ghost" })} aria-pressed={source === "semble"} onClick={() => setSource("semble")}><BookOpen className="size-4" />Semble</button><button type="button" className={buttonVariants({ variant: source === "margin" ? "secondary" : "ghost" })} aria-pressed={source === "margin"} onClick={() => setSource("margin")}><Highlighter className="size-4" />Margin</button></nav>
      {source === "semble" ? <>
        {semble.isPending ? <p role="status">Loading Semble Collections…</p> : null}
        {semble.error ? <div role="alert" className="space-y-2"><p>{semble.error.message}</p><Button variant="outline" onClick={() => { void semble.refetch(); }}>Retry Semble</Button></div> : null}
        {!semble.isPending && !semble.error && !semble.collections.length ? <div className="py-12 text-center"><h2 className="font-semibold">No Semble Collections Found</h2><p className="mt-2 text-sm text-muted-foreground">Collections saved by this account will appear here.</p></div> : null}
        <div className="grid gap-4">{semble.collections.map(collection => selection?.did === did && selection.uri === collection.uri ?
          <ResearchCollection key={collection.uri} collection={collection} onClose={() => setSelection(undefined)} /> :
          <article key={collection.uri} className="min-w-0 space-y-2 rounded-xl border bg-card/60 p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="font-semibold">{collection.name}</h2><span className="text-xs text-muted-foreground">{collection.accessType === "CLOSED" ? "Closed" : "Open"} · {collection.cardCount} Items</span></div>{collection.description ? <p className="text-sm text-muted-foreground">{collection.description}</p> : null}<Button variant="outline" size="sm" aria-label={`Open ${collection.name}`} onClick={() => setSelection({ did, uri: collection.uri })}>Open Collection</Button></article>
        )}</div>
        {semble.hasNextPage ? <Button variant="outline" disabled={semble.isFetchingNextPage} onClick={() => { void semble.fetchNextPage(); }}>Load More Collections</Button> : null}
      </> : <>
        {margin.isPending ? <p role="status">Loading Margin Annotations…</p> : null}
        {margin.error ? <div role="alert" className="space-y-2"><p>{margin.error.message}</p><Button variant="outline" onClick={() => { void margin.refetch(); }}>Retry Margin</Button></div> : null}
        {!margin.isPending && !margin.error && !margin.data?.length ? <div className="py-12 text-center"><h2 className="font-semibold">No Margin Annotations Found</h2><p className="mt-2 text-sm text-muted-foreground">Notes, highlights, and bookmarks from this account will appear here.</p></div> : null}
        <div className="grid gap-4 md:grid-cols-2">{margin.data?.map(annotation => <article key={annotation.uri} className="min-w-0 space-y-3 break-words rounded-xl border bg-card/60 p-4"><header className="space-y-2"><div className="flex flex-wrap justify-between gap-2 text-xs text-muted-foreground"><span className="capitalize">{annotation.motivation}</span><time dateTime={annotation.createdAt}>{new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(new Date(annotation.createdAt))}</time></div><h2 className="font-semibold">{annotation.title || annotation.source}</h2></header>{annotation.quote ? <blockquote className="border-l-2 border-primary/40 pl-3 text-sm italic">“{annotation.quote}”</blockquote> : null}{annotation.body ? <p className="whitespace-pre-wrap text-sm">{annotation.body}</p> : null}{annotation.tags.length ? <p className="text-xs text-muted-foreground">{annotation.tags.join(" · ")}</p> : null}<ResearchSourceLink url={annotation.source} /></article>)}</div>
      </>}
    </div>
  </section>;
}
