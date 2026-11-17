"use client";

import { AppBskyFeedDefs, moderateProfile, type ModerationOpts } from "@atproto/api";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useComposerDraft } from "@/hooks/useComposerDraft";
import { useAuth } from "@/hooks/useAuth";
import { useBrandAccounts, useBrandAccountSession } from "@/hooks/useBrandAccounts";
import { getStoredOAuthDid } from "@/lib/auth";
import { createBlueskyPost } from "@/lib/blueskyComposeClient";
import { getBlueskyPostThread } from "@/lib/blueskyPostClient";
import { getBlueskySocialCatalog } from "@/lib/blueskySocialClient";
import { newThreadPublication, publishBlueskyThread, validThreadText, type ThreadPublication, type ThreadSegment } from "@/lib/blueskyThreadClient";
import type { ComposeLinkPreview } from "@/lib/blueskyExternalEmbed";
import { missingMediaAlt } from "@/lib/composerMedia";
import { replaceSocialDraftMedia } from "@/lib/socialDraftConversion";
import { validateSocialPostRecord } from "@/lib/socialPostRecord";
import { refreshSocialPostQueries } from "@/lib/socialPostRefresh";
import { composerErrorMessage } from "@/lib/composerErrorMessage";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";
import { handlePostComposerShortcut } from "@/lib/postComposerShortcut";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "@/components/ui/dialog";
import { SocialPermissionRecovery } from "./SocialPermissionRecovery";
import { SocialPostCard } from "./SocialPostCard";
import { SocialComposeAccountPicker } from "./SocialComposeAccountPicker";
import { SocialThreadSegment } from "./SocialThreadSegment";
import { moderateSocialPost } from "./socialModeration";

type ComposeProps = {
  post: AppBskyFeedDefs.PostView;
  moderation: ModerationOpts;
  mode: "reply" | "quote";
  trigger?: (openComposer: () => void, disabled: boolean) => ReactNode;
  initialActor?: string;
  onClose?: () => void;
};

export function SocialPostComposeDialog(props: ComposeProps) {
  const { session } = useAuth();
  return <SocialAccountPostComposeDialog key={`${session?.did ?? ""}:${props.post.uri}:${props.mode}:${props.initialActor ?? ""}`} {...props} />;
}

function SocialAccountPostComposeDialog({ post, moderation, mode, trigger, initialActor, onClose }: ComposeProps) {
  const { session, getOAuthSession } = useAuth();
  const cache = useQueryClient();
  const [open, setOpen] = useState(!!initialActor);
  const [pending, setPending] = useState(false);
  const [published, setPublished] = useState(false);
  const stopRefresh = useRef<(() => void) | undefined>(undefined);
  const active = useRef(false);
  useLayoutEffect(() => {
    active.current = true;
    return () => { active.current = false; stopRefresh.current?.(); };
  }, []);
  const identity = moderateProfile(post.author, moderation).ui("displayName");
  const recipient = identity.filter || identity.blur || identity.noOverride ? "Account" : `@${post.author.handle}`;
  const quote = mode === "quote";
  const disabled = !session || moderation.userDid !== session.did || (quote ? post.viewer?.embeddingDisabled : post.viewer?.replyDisabled) || !validateSocialPostRecord(post.record).success;

  function changeOpen(value: boolean) {
    if (pending) return;
    if (value) setPublished(false);
    setOpen(value);
    if (!value) onClose?.();
  }

  function finish(ownerDid: string, actorDid: string) {
    setPending(false);
    setPublished(true);
    setOpen(false);
    if (actorDid === ownerDid) {
      stopRefresh.current?.();
      stopRefresh.current = refreshSocialPostQueries(cache, ownerDid, () => active.current && getStoredOAuthDid() === ownerDid && getOAuthSession()?.did === ownerDid);
    } else {
      void cache.invalidateQueries({ queryKey: ["brandAccounts", ownerDid, actorDid] });
    }
    onClose?.();
  }

  return <>
    <Dialog open={open} onOpenChange={changeOpen}>
      {trigger?.(() => { if (!disabled) changeOpen(true); }, !!disabled)}
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg" showCloseButton={!pending}>
        <DialogTitle>{quote ? "Quote Post" : "Reply"}</DialogTitle>
        <DialogDescription>{quote ? "Quoting" : "Replying To"} {recipient}</DialogDescription>
        {open && session ? <PostComposerAccounts ownerDid={session.did} initialActor={initialActor} post={post}
          moderation={moderation} mode={mode} pending={pending} setPending={setPending}
          finish={actorDid => finish(session.did, actorDid)} /> : null}
      </DialogContent>
    </Dialog>
    {published ? <p role="status" className="mt-2 text-xs text-muted-foreground">{quote ? "Quote Post Published" : "Reply Published"}</p> : null}
  </>;
}

type EditorProps = Pick<ComposeProps, "post" | "moderation" | "mode"> & {
  ownerDid: string;
  pending: boolean;
  setPending: (pending: boolean) => void;
  finish: (actorDid: string) => void;
};

function PostComposerAccounts({ initialActor, ...props }: EditorProps & { initialActor?: string }) {
  const [actorDid, setActorDid] = useState(initialActor ?? props.ownerDid);
  const accounts = useBrandAccounts(props.ownerDid, props.mode === "reply");
  return <PostComposerEditor {...props} initialActor={initialActor} actorDid={actorDid} accounts={accounts} setActorDid={setActorDid} />;
}

function PostComposerEditor({ post, moderation, mode, ownerDid, initialActor, actorDid, accounts, setActorDid, pending, setPending, finish }: EditorProps & {
  initialActor?: string;
  actorDid: string;
  accounts: string[];
  setActorDid: (did: string) => void;
}) {
  const { getOAuthSession } = useAuth();
  const personal = actorDid === ownerDid;
  const available = personal || accounts.includes(actorDid);
  const connection = useBrandAccountSession(ownerDid, actorDid, !personal && available);
  const oauth = personal ? getOAuthSession() : connection.session;
  const quote = mode === "quote";
  const needsActorTarget = !personal || moderation.userDid !== actorDid;
  const active = useRef(false);
  const busy = useRef(false);
  useLayoutEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);

  function assertCurrentAccount() {
    if (!active.current || getStoredOAuthDid() !== ownerDid || oauth?.did !== actorDid ||
      (personal && getOAuthSession() !== oauth)) throw new Error("Your account changed. Please reopen the composer.");
    if (!personal) connection.assertCurrentActor();
  }

  const catalog = useQuery({
    queryKey: personal ? ["notificationReplyCatalog", ownerDid] : ["brandAccounts", ownerDid, actorDid, connection.generation, "catalog"],
    enabled: needsActorTarget && available && oauth?.did === actorDid,
    queryFn: async ({ signal }) => {
      assertCurrentAccount();
      const result = await getBlueskySocialCatalog(oauth!, signal, { includeFeeds: false });
      assertCurrentAccount();
      return result;
    },
    retry: false,
  });
  const actorModeration = needsActorTarget ? catalog.data?.moderation : moderation;

  const [segments, setSegments] = useState<ThreadSegment[]>([{ id: "first", text: "" }]);
  const [locked, setLocked] = useState(false);
  const [completed, setCompleted] = useState(0);
  const [discardRequested, setDiscardRequested] = useState(false);
  const [altWarning, setAltWarning] = useState(false);
  const [mediaProgress, setMediaProgress] = useState<string>();
  const [error, setError] = useState<string>();
  const previewResolvers = useRef(new Map<string, () => Promise<ComposeLinkPreview | undefined>>());
  const publication = useRef<ThreadPublication | undefined>(undefined);
  const context = initialActor ? `${mode}:${initialActor}:${post.uri}` : `${mode}:${post.uri}`;
  const draft = useComposerDraft(ownerDid, context, segments, publication, restored => {
    setSegments(restored);
    if (publication.current?.segments.some(item => item.attempted)) {
      setActorDid(publication.current.did);
      setLocked(true);
    }
    setCompleted(publication.current?.segments.filter(item => item.result).length ?? 0);
  });
  const valid = segments.every(segment => validThreadText(segment.text, segment.media));
  const thread = segments.length > 1;
  const disabled = pending || locked || !draft.loaded;

  function updateSegment(id: string, update: (segment: ThreadSegment) => ThreadSegment) {
    setSegments(previous => previous.map(segment => segment.id === id ? update(segment) : segment));
  }

  function moveSegment(index: number, direction: -1 | 1) {
    setSegments(previous => {
      const next = [...previous];
      [next[index], next[index + direction]] = [next[index + direction], next[index]];
      return next;
    });
  }

  async function publish(allowMissingAlt = false) {
    if (busy.current || !valid || !available || !draft.loaded || actorModeration?.userDid !== actorDid || catalog.isError) return;
    if (!allowMissingAlt && !publication.current?.segments.some(item => item.attempted) && segments.some(segment => missingMediaAlt(segment.media))) {
      setAltWarning(true);
      return;
    }
    setAltWarning(false);
    busy.current = true;
    setPending(true);
    setError(undefined);
    try {
      if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
      assertCurrentAccount();
      if (!publication.current) {
        let target = post;
        if (needsActorTarget) {
          const { thread: targetThread } = await getBlueskyPostThread({ session: oauth, moderation: actorModeration, uri: post.uri, depth: 0, parentHeight: 0 });
          assertCurrentAccount();
          if (!AppBskyFeedDefs.isThreadViewPost(targetThread) || targetThread.post.uri !== post.uri) throw new Error("The Reply Target Is Unavailable.");
          target = targetThread.post;
          const visibility = moderateSocialPost(target, actorModeration).ui("contentList");
          if (visibility.filter || visibility.blur || visibility.noOverride) throw new Error("The Reply Target Is Unavailable For This Account.");
        }
        const previews = await Promise.all(segments.map(segment => previewResolvers.current.get(segment.id)?.()));
        assertCurrentAccount();
        publication.current = newThreadPublication(actorDid, segments.map((segment, index) => ({
          ...segment,
          linkPreview: previews[index],
          replyTo: index === 0 && !quote ? target : undefined,
          quoteTo: index === 0 && quote ? target : undefined,
        })));
      }
      setLocked(true);
      if (thread) {
        await publishBlueskyThread({ session: oauth, publication: publication.current, assertCurrentAccount, onProgress: setCompleted, onMediaProgress: setMediaProgress, checkpoint: draft.checkpoint });
      } else {
        await createBlueskyPost({ session: oauth, ...publication.current.segments[0].draft, publication: publication.current, checkpoint: draft.checkpoint, onMediaProgress: setMediaProgress, assertCurrentAccount });
      }
      assertCurrentAccount();
      await draft.clear();
      if (active.current) finish(actorDid);
    } catch (failure) {
      if (!publication.current?.segments.some(item => item.attempted)) {
        publication.current = undefined;
        setLocked(false);
      }
      if (active.current) setError(composerErrorMessage(failure, thread));
    } finally {
      busy.current = false;
      if (active.current) {
        setMediaProgress(undefined);
        setPending(false);
      }
    }
  }

  return <>
    {!quote ? <SocialComposeAccountPicker label="Reply As" ownerDid={ownerDid} accounts={accounts} actorDid={actorDid} disabled={disabled} onChange={did => { setActorDid(did); setError(undefined); }} /> : null}
    {locked ? <p role="status">{thread ? `${completed} of ${segments.length} Posts Published. Retry Continues This Thread.` : "Retry Continues The Same Post Without Duplicating It."}</p> : null}
    {locked && !pending ? discardRequested ? <div className="rounded-xl border p-3 text-sm">
      <p>Discard This Draft? A Post With An Unconfirmed Result May Already Be Published.</p>
      <Button type="button" variant="destructive" onClick={() => { void draft.clear().then(() => {
        publication.current = undefined;
        setLocked(false);
        setSegments([{ id: "first", text: "" }]);
        setCompleted(0);
        setError(undefined);
        setDiscardRequested(false);
      }).catch(() => setError("Draft Could Not Be Cleared")); }}>Discard Draft</Button>
      <Button type="button" variant="outline" onClick={() => setDiscardRequested(false)}>Keep Draft</Button>
    </div> : <Button type="button" variant="outline" onClick={() => setDiscardRequested(true)}>Start A New Draft</Button> : null}
    {!available ? <p role="alert">Choose a linked account to reply.</p>
      : connection.error || catalog.isError ? <div className="space-y-2">
        <p role="alert" className="text-sm text-destructive">The Selected Account Could Not Be Loaded.</p>
        <Button variant="outline" onClick={() => { if (connection.error) connection.retry(); else void catalog.refetch(); }}>Retry</Button>
      </div>
      : needsActorTarget && personal && !oauth ? <><p role="alert">{SCOPE_RECOVERY_MESSAGE}</p><SocialPermissionRecovery /></>
      : actorModeration?.userDid !== actorDid || (!personal && !oauth) ? <p role="status" className="text-sm text-muted-foreground">Loading Selected Account…</p>
      : <form className="flex min-w-0 flex-col gap-3" onKeyDown={handlePostComposerShortcut} onSubmit={event => { event.preventDefault(); void publish(); }}>
        {quote ? <div className="max-h-48 overflow-y-auto rounded-xl border p-3"><SocialPostCard item={{ post }} moderation={moderation} embedded /></div> : null}
        {segments.map((segment, index) => <SocialThreadSegment key={`${actorDid}:${segment.id}`} index={index} total={segments.length}
          firstLabel={quote ? "Quote Text" : "Reply Text"} firstPlaceholder={quote ? "Add a Comment…" : "Write a Reply…"} preserveFirst
          suggestionActor={!personal && oauth ? { ownerDid, session: oauth, moderation: actorModeration, assertCurrentActor: assertCurrentAccount } : undefined}
          text={segment.text} media={segment.media} disabled={disabled} session={oauth} open
          savedPreview={segment.linkPreview} removedUrl={segment.removedLinkUrl}
          onChange={text => updateSegment(segment.id, previous => ({ ...previous, text }))}
          updateMedia={change => updateSegment(segment.id, previous => replaceSocialDraftMedia(previous, change(previous.media)))}
          onMove={direction => moveSegment(index, direction)}
          onDelete={() => setSegments(previous => previous.filter(item => item.id !== segment.id))}
          rememberRemovedUrl={removedLinkUrl => updateSegment(segment.id, previous => ({ ...previous, removedLinkUrl }))}
          registerPreview={resolve => { if (resolve) previewResolvers.current.set(segment.id, resolve); else previewResolvers.current.delete(segment.id); }} />)}
        {!quote ? <Button type="button" variant="outline" disabled={disabled} onClick={() => setSegments(previous => [...previous, { id: crypto.randomUUID(), text: "" }])}>Add Post</Button> : null}
        {draft.storageError ? <div role="alert">{draft.storageError}<Button type="button" variant="outline" onClick={() => void draft.checkpoint().catch(() => {})}>Retry Saving Draft</Button></div> : null}
        {mediaProgress ? <p role="status">{mediaProgress}</p> : null}
        {altWarning ? <div role="alert" className="rounded-xl border p-3 text-sm"><p>A Post Has An Attachment With No Alt Text. Add A Description Before Posting.</p><Button type="button" variant="outline" onClick={() => setAltWarning(false)}>Add Description</Button><Button type="button" onClick={() => void publish(true)}>Post Without Description</Button></div> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {error === SCOPE_RECOVERY_MESSAGE ? <SocialPermissionRecovery /> : null}
        <DialogFooter>
          <DialogClose disabled={pending} className="rounded-xl border px-3 py-2">Cancel</DialogClose>
          <Button type="submit" disabled={pending || !draft.loaded || !valid}>{pending ? "Publishing…" : quote ? "Post Quote" : thread ? "Post Thread" : "Post Reply"}</Button>
        </DialogFooter>
      </form>}
  </>;
}
