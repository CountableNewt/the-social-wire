"use client";

import { useLayoutEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AppBskyFeedDefs } from "@atproto/api";
import { BrandPostComposer } from "@/components/BrandAccounts/BrandPostComposer";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/hooks/useAuth";
import { useBrandAccountSession } from "@/hooks/useBrandAccounts";
import { getStoredOAuthDid } from "@/lib/auth";
import { createBlueskyPost } from "@/lib/blueskyComposeClient";
import type { ComposeLinkPreview } from "@/lib/blueskyExternalEmbed";
import { getBlueskyPostThread } from "@/lib/blueskyPostClient";
import { getBlueskySocialCatalog } from "@/lib/blueskySocialClient";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";
import { moderateSocialPost } from "./socialModeration";
import { SocialPermissionRecovery } from "./SocialPermissionRecovery";

export function AccountReplyContent({ ownerDid, actorDid, parent, draft, pending, setPending, close }: {
  ownerDid: string;
  actorDid: string;
  parent: AppBskyFeedDefs.PostView;
  draft: { text: string; setText: (text: string) => void };
  pending: boolean;
  setPending: (pending: boolean) => void;
  close: () => void;
}) {
  const { session: viewer, getOAuthSession } = useAuth();
  const personal = actorDid === ownerDid;
  const connection = useBrandAccountSession(ownerDid, actorDid, !personal);
  const session = personal ? getOAuthSession() : connection.session;
  const cache = useQueryClient();
  const active = useRef(false);
  const busy = useRef(false);
  useLayoutEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);

  function assertCurrentActor() {
    if (!active.current || viewer?.did !== ownerDid || getStoredOAuthDid() !== ownerDid || session?.did !== actorDid) {
      throw new Error("Your account changed. Please reopen the composer.");
    }
    if (personal) {
      if (getOAuthSession() !== session) throw new Error("Your account changed. Please reopen the composer.");
    } else connection.assertCurrentActor();
  }

  const catalogKey = ["brandAccounts", ownerDid, actorDid, connection.generation, "catalog"] as const;
  const catalog = useQuery({
    queryKey: personal ? ["notificationReplyCatalog", ownerDid] : catalogKey,
    enabled: session?.did === actorDid && viewer?.did === ownerDid,
    queryFn: async ({ signal }) => {
      assertCurrentActor();
      const data = await getBlueskySocialCatalog(session!, signal, { includeFeeds: false });
      assertCurrentActor();
      return data;
    },
    retry: false,
  });
  const moderation = !catalog.isError && catalog.data?.moderation.userDid === actorDid ? catalog.data.moderation : undefined;

  async function publish(text: string, linkPreview?: ComposeLinkPreview) {
    if (busy.current || !session || !moderation) throw new Error("The Selected Account Is Unavailable.");
    busy.current = true;
    setPending(true);
    try {
      assertCurrentActor();
      const { thread } = await getBlueskyPostThread({ session, moderation, uri: parent.uri, depth: 0, parentHeight: 0 });
      assertCurrentActor();
      if (!AppBskyFeedDefs.isThreadViewPost(thread) || thread.post.uri !== parent.uri) throw new Error("The Reply Target Is Unavailable.");
      const visibility = moderateSocialPost(thread.post, moderation).ui("contentList");
      if (visibility.filter || visibility.blur || visibility.noOverride) throw new Error("The Reply Target Is Unavailable For This Account.");
      await createBlueskyPost({ session, text, linkPreview, replyTo: thread.post, assertCurrentAccount: assertCurrentActor });
      void cache.invalidateQueries({ queryKey: personal ? ["blueskySocial", ownerDid] : ["brandAccounts", ownerDid, actorDid] });
    } finally {
      busy.current = false;
      setPending(false);
    }
  }

  if (personal && !session) return <><p role="alert">{SCOPE_RECOVERY_MESSAGE}</p><SocialPermissionRecovery /></>;
  if (connection.error || catalog.isError) return <div className="space-y-2">
    <p role="alert" className="text-sm text-destructive">The Selected Account Could Not Be Loaded.</p>
    <Button variant="outline" onClick={() => { if (connection.error) connection.retry(); else void catalog.refetch(); }}>Retry</Button>
  </div>;
  if (!session || !moderation) return <p role="status" className="text-sm text-muted-foreground">Loading Selected Account…</p>;
  return <BrandPostComposer inline showIdentity={false} ownerDid={ownerDid} session={session} moderation={moderation}
    profile={undefined} parent={parent} draft={draft} pending={pending} publish={publish} close={close} assertCurrentActor={assertCurrentActor} />;
}
