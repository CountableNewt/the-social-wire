"use client";

import { useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import type { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";
import { isRssPublicationId, normalizedFeedUrlFromRssPublicationId } from "@/lib/rssFeedCore";

export function UnavailableFeedsDialog({ warning, publications, onUnsubscribe }: {
  warning: PublicationSourceRefreshError;
  publications: readonly DiscoveredPublication[];
  onUnsubscribe: (publication: DiscoveredPublication) => Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState<DiscoveredPublication | null>(null);
  const [removed, setRemoved] = useState<ReadonlySet<string>>(new Set());
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const confirming = useRef(false);
  const rowsById = new Map(publications.map(publication => [publication.publicationId, publication]));
  const ids = [...new Set(warning.unavailablePublicationIds)].filter(id => !removed.has(id));

  async function confirm() {
    if (!selected || confirming.current) return;
    const current = rowsById.get(selected.publicationId);
    if (!current || current.subscriptionPublicationId !== selected.subscriptionPublicationId || !ids.includes(selected.publicationId)) {
      setError("This subscription has changed. Close this dialog and refresh before trying again.");
      return;
    }
    confirming.current = true;
    setPending(true);
    setError(null);
    try {
      await onUnsubscribe(current);
      setRemoved(current => new Set([...current, selected.publicationId]));
      setSelected(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could Not Unsubscribe. Please Retry.");
    } finally {
      confirming.current = false;
      setPending(false);
    }
  }

  return <>
    <Button type="button" variant="outline" size="sm" onClick={() => {
      setSelected(null);
      setError(null);
      setOpen(true);
    }}>View Unavailable Feeds</Button>
    <Dialog open={open} onOpenChange={value => { if (!confirming.current) setOpen(value); }}>
      <DialogContent showCloseButton={!pending}>
        <DialogHeader>
          <DialogTitle>{selected ? "Unsubscribe?" : "Unavailable Feeds"}</DialogTitle>
          <DialogDescription>{selected
            ? `Stop receiving articles from “${selected.title}” in Subscribed? You can add this feed again later from Add Publication.`
            : "Your articles were updated, but these feeds could not be refreshed. They may be temporarily unavailable or have moved. You can keep them and try again later, or unsubscribe."}</DialogDescription>
        </DialogHeader>
        {selected ? <p className="break-all text-sm text-muted-foreground">{normalizedFeedUrlFromRssPublicationId(selected.publicationId)}</p> :
          <ul className="flex max-h-80 flex-col gap-3 overflow-y-auto">
            {ids.map(id => {
              const publication = rowsById.get(id);
              const feedUrl = normalizedFeedUrlFromRssPublicationId(id);
              const canUnsubscribe = publication && isRssPublicationId(id) && Boolean(publication.subscriptionPublicationId);
              return <li key={id} className="flex flex-wrap items-center gap-2 rounded-md border p-3">
                <div className="min-w-0 flex-1">
                  <p className="break-words text-sm font-medium">{publication?.title || feedUrl || "Unavailable Feed"}</p>
                  {feedUrl && publication?.title ? <p className="break-all text-xs text-muted-foreground">{feedUrl}</p> : null}
                  {!canUnsubscribe ? <p className="text-xs text-muted-foreground">No current subscription found. Refresh to update this list.</p> : null}
                </div>
                {canUnsubscribe ? <Button type="button" variant="outline" size="sm" aria-label={`Unsubscribe From ${publication.title}`} onClick={() => {
                  setError(null);
                  setSelected(publication);
                }}>Unsubscribe</Button> : null}
              </li>;
            })}
            {!warning.unavailablePublicationIds.length ? warning.hosts.map(host => <li key={host} className="break-all text-sm">{host}<p className="text-xs text-muted-foreground">Refresh to identify the affected subscription before unsubscribing.</p></li>) : null}
          </ul>}
        {!selected && warning.unavailablePublicationIds.length > 0 && !ids.length ? <p role="status" className="text-sm">You’ve unsubscribed from all feeds in this list.</p> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        <DialogFooter>
          <DialogClose render={<Button variant="outline" disabled={pending} />}>{selected ? "Cancel" : "Close"}</DialogClose>
          {selected ? <Button type="button" variant="destructive" disabled={pending} onClick={() => void confirm()}>{pending ? "Unsubscribing…" : "Unsubscribe"}</Button> : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </>;
}
