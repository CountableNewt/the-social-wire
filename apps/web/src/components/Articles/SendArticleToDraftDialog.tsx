"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { useAuth } from "@/hooks/useAuth";
import { listArticlePublications } from "@/lib/articles/articlePublishingClient";
import { articlePublicationDisabledReason, PCKT_EXTERNAL_EDITORS_MESSAGE } from "@/lib/articles/articlePublicationCapabilities";
import { createArticleDraftFromLink } from "@/lib/articles/createArticleDraftFromLink";
import { syncArticleDraft } from "@/lib/articles/articleDraftSync";
import type { ArticleDraftActor } from "@/lib/articles/articleDraftCloudTypes";
import type { ArticleDraft } from "@/lib/articles/articleDraftTypes";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "@/components/ui/dialog";

export function SendArticleToDraftDialog({ url, close }: { url: string; close: () => void }) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const router = useRouter();
  const [viewer] = useState(() => ({ did: session?.did, epoch: oauthSessionReloadSeq }));
  const auth = useRef({ getOAuthSession, oauthSessionReloadSeq });
  useLayoutEffect(() => { auth.current = { getOAuthSession, oauthSessionReloadSeq }; }, [getOAuthSession, oauthSessionReloadSeq]);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const actor = (): ArticleDraftActor => {
    const oauth = getOAuthSession();
    if (!oauth || !viewer.did || oauth.did !== viewer.did || oauthSessionReloadSeq !== viewer.epoch) throw new Error("Your Account Changed. Reopen Send to Draft.");
    return { session: oauth, viewerDid: viewer.did, assertCurrentActor: () => {
      if (!mounted.current || auth.current.getOAuthSession() !== oauth || auth.current.oauthSessionReloadSeq !== viewer.epoch) throw new Error("Your Account Changed. Reopen Send to Draft.");
    } };
  };
  const publications = useQuery({ queryKey: ["articles", viewer.did, "publications", viewer.epoch], queryFn: ({ signal }) => listArticlePublications(actor().session, signal), retry: false });
  const [publicationUri, setPublicationUri] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const savedDraft = useRef<ArticleDraft | undefined>(undefined);
  const [saved, setSaved] = useState<ArticleDraft>();
  const lock = useRef(false);
  const selected = publications.data?.find(value => value.uri === publicationUri);
  const accountChanged = session?.did !== viewer.did || oauthSessionReloadSeq !== viewer.epoch;
  const openDraft = () => {
    try { actor().assertCurrentActor?.(); if (savedDraft.current) router.push(`/articles?draft=${encodeURIComponent(savedDraft.current.id)}`); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "The Draft Could Not Be Opened."); }
  };
  const submit = async () => {
    if (lock.current) return;
    lock.current = true; setPending(true); setError(undefined);
    try {
      const context = actor();
      if (!savedDraft.current) {
        if (!selected) throw new Error("Choose a Publication.");
        savedDraft.current = await createArticleDraftFromLink(context, selected, url);
        setSaved(savedDraft.current);
      }
      const synced = await syncArticleDraft(context, savedDraft.current.id);
      context.assertCurrentActor?.();
      if (!synced) throw new Error("This Draft Is No Longer Available.");
      router.push(`/articles?draft=${encodeURIComponent(savedDraft.current.id)}`);
    } catch (failure) {
      if (mounted.current) setError(failure instanceof Error ? failure.message : "The Draft Could Not Be Created.");
    } finally { lock.current = false; if (mounted.current) setPending(false); }
  };
  return <Dialog open onOpenChange={open => { if (!open && !pending) close(); }}><DialogContent showCloseButton={!pending}>
    <DialogTitle>Send to Draft</DialogTitle>
    <DialogDescription>Start a New Blog Post With This Article’s Link. Choose the Publication for Your Private Draft.</DialogDescription>
    <label className="block space-y-2 text-sm"><span>Publication</span><div className="relative"><select aria-label="Draft Publication" value={publicationUri} disabled={pending || !!saved || accountChanged} onChange={event => setPublicationUri(event.target.value)} className="w-full appearance-none rounded-md border bg-background py-2 pl-3 pr-9"><option value="">Select a Publication</option>{publications.data?.map(publication => <option key={publication.uri} value={publication.uri} disabled={!!articlePublicationDisabledReason(publication)}>{publication.name}{articlePublicationDisabledReason(publication) ? ` — ${PCKT_EXTERNAL_EDITORS_MESSAGE}` : ""}</option>)}</select><ChevronDown className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2" aria-hidden="true" /></div></label>
    {publications.isPending ? <p role="status" className="text-sm">Loading Publications…</p> : publications.error ? <div role="alert" className="text-sm text-destructive">Publications Could Not Be Loaded. <Button variant="outline" size="sm" onClick={() => { void publications.refetch(); }}>Retry</Button></div> : !publications.data?.some(publication => !articlePublicationDisabledReason(publication)) ? <p className="text-sm text-muted-foreground">Create a Supported Publication Before Sending an Article to Drafts.</p> : null}
    {accountChanged ? <p role="alert" className="text-sm text-destructive">Your Account Changed. Reopen Send to Draft.</p> : error ? <div role="alert" className="space-y-2 text-sm text-destructive">{saved ? <p>Your Draft Is Saved in This Browser. Retry Private Sync or Open It to Continue.</p> : null}<p>{error}</p></div> : null}
    <DialogFooter>{saved && error && !accountChanged ? <Button variant="outline" disabled={pending} onClick={openDraft}>Open Draft</Button> : null}<DialogClose render={<Button variant="outline" disabled={pending} />}>Cancel</DialogClose><Button disabled={pending || accountChanged || (!saved && (!selected || !!articlePublicationDisabledReason(selected)))} onClick={() => { void submit(); }}>{pending ? "Creating Draft…" : saved ? "Retry Private Sync" : "Create Draft"}</Button></DialogFooter>
  </DialogContent></Dialog>;
}
