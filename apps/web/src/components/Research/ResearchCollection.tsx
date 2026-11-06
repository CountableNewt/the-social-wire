"use client";

import { Button } from "@/components/ui/button";
import { useSembleCollectionItems } from "@/hooks/useSembleReadLater";
import type { SembleCollection } from "@/lib/semble";
import { ResearchSourceLink } from "./ResearchSourceLink";

export function ResearchCollection({ collection }: { collection: SembleCollection }) {
  const query = useSembleCollectionItems(collection.uri);
  return <article className="min-w-0 rounded-xl border bg-card/60">
    <header className="space-y-2 border-b p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="font-semibold">{collection.name}</h2><span className="text-xs text-muted-foreground">{collection.accessType === "CLOSED" ? "Closed" : "Open"} · {collection.cardCount} Items</span></div>{collection.description ? <p className="text-sm text-muted-foreground">{collection.description}</p> : null}</header>
    {query.isPending ? <p role="status" className="p-4 text-sm text-muted-foreground">Loading Collection…</p> : null}
    {query.error ? <div role="alert" className="space-y-2 p-4 text-sm"><p>{query.error.message}</p><Button size="sm" variant="outline" onClick={() => { void query.refetch(); }}>Retry Collection</Button></div> : null}
    {!query.isPending && !query.error && !query.items.length ? <p className="p-4 text-sm text-muted-foreground">This collection is empty.</p> : null}
    <div className="divide-y">{query.items.map(item => <section key={item.id} className="space-y-2 break-words p-4"><h3 className="text-sm font-semibold">{item.title || item.note?.text || item.url || "Untitled Saved Item"}</h3>{item.siteName ? <p className="text-xs text-muted-foreground">{item.siteName}</p> : null}{item.description ? <p className="text-sm text-muted-foreground">{item.description}</p> : null}{item.note?.text && item.note.text !== item.title ? <p className="whitespace-pre-wrap text-sm">{item.note.text}</p> : null}<ResearchSourceLink url={item.url} /></section>)}</div>
    {query.hasNextPage ? <Button variant="ghost" className="m-3" disabled={query.isFetchingNextPage} onClick={() => { void query.fetchNextPage(); }}>Load More Items</Button> : null}
  </article>;
}
