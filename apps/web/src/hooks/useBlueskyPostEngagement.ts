"use client";

import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { AppBskyFeedDefs, ModerationOpts } from "@atproto/api";
import { useAuth } from "@/hooks/useAuth";
import { setBlueskyPostEngagement, type SocialEngagementKind, type SocialPostEngagement } from "@/lib/blueskyPostClient";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";
import { invalidateSocialPostQueries } from "@/lib/invalidateSocialPostQueries";

type EngagementOverride = { uri?: string; pending: boolean };
type EngagementOverrides = Partial<Record<SocialEngagementKind, EngagementOverride>>;
const NO_OVERRIDES: EngagementOverrides = {};

function displayEngagement(uri: string | undefined, count: number, override?: EngagementOverride) {
  if (!override) return { uri, count };
  const delta = Number(!!override.uri) - Number(!!uri);
  return { uri: override.uri, count: Math.max(0, count + delta) };
}

type Action = { kind: SocialEngagementKind; did: string; key: readonly unknown[]; existingUri?: string; uri: string; cid: string; moderation: ModerationOpts };

export function useBlueskyPostEngagement(post: AppBskyFeedDefs.PostView, moderation: ModerationOpts | undefined) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const client = useQueryClient();
  const busy = useRef<string | null>(null);
  const viewerDid = useRef(session?.did);
  useEffect(() => { viewerDid.current = session?.did; }, [session?.did]);
  const did = session?.did ?? "";
  const postUri = post.uri;
  const key = ["blueskySocial", did, "engagement", oauthSessionReloadSeq, postUri] as const;
  const { data: overrides } = useQuery({ queryKey: key, queryFn: () => NO_OVERRIDES, initialData: NO_OVERRIDES, enabled: false });
  const sourceLike = post.viewer?.like;
  const sourceRepost = post.viewer?.repost;
  const like = displayEngagement(sourceLike, post.likeCount ?? 0, overrides.like);
  const repost = displayEngagement(sourceRepost, post.repostCount ?? 0, overrides.repost);
  const engagement: SocialPostEngagement = { like: like.uri, likeCount: like.count, repost: repost.uri, repostCount: repost.count };

  // Keep the local change while the App View catches up, then resume using its viewer state.
  useEffect(() => {
    const likeConfirmed = overrides.like && !overrides.like.pending && overrides.like.uri === sourceLike;
    const repostConfirmed = overrides.repost && !overrides.repost.pending && overrides.repost.uri === sourceRepost;
    if (!likeConfirmed && !repostConfirmed) return;
    client.setQueryData<EngagementOverrides>(["blueskySocial", did, "engagement", oauthSessionReloadSeq, postUri], current => {
      if (!current) return current;
      return {
        like: likeConfirmed && current.like === overrides.like ? undefined : current.like,
        repost: repostConfirmed && current.repost === overrides.repost ? undefined : current.repost,
      };
    });
  }, [client, did, oauthSessionReloadSeq, postUri, overrides, sourceLike, sourceRepost]);
  const requireSession = (action: Action) => {
    const oauth = getOAuthSession();
    if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
    if (oauth.did !== action.did || viewerDid.current !== action.did || action.moderation.userDid !== action.did) throw new Error("Your account changed. Please reload this post.");
    return oauth;
  };
  const mutation = useMutation({
    mutationFn: (action: Action) => setBlueskyPostEngagement({ ...action, session: requireSession(action) }),
    onMutate: action => {
      requireSession(action);
      const previous = client.getQueryData<EngagementOverrides>(action.key) ?? NO_OVERRIDES;
      if (previous.like?.pending || previous.repost?.pending) throw new Error("A post action is already in progress.");
      client.setQueryData(action.key, { ...previous, [action.kind]: { uri: action.existingUri ? undefined : "pending", pending: true } });
      return { override: previous[action.kind] };
    },
    onSuccess: (uri, action) => {
      if (viewerDid.current !== action.did || getOAuthSession()?.did !== action.did) return;
      client.setQueryData<EngagementOverrides>(action.key, current => current ? { ...current, [action.kind]: { uri, pending: false } } : current);
      void invalidateSocialPostQueries(client, action.did, new Set(["timeline", "profileTimeline", "thread"]));
    },
    onError: (_error, action, previous) => {
      if (previous && viewerDid.current === action.did && getOAuthSession()?.did === action.did) {
        client.setQueryData<EngagementOverrides>(action.key, current => current ? { ...current, [action.kind]: previous.override } : current);
      }
    },
    onSettled: (_data, _error, action) => { if (busy.current === action.did) busy.current = null; },
  });
  const toggle = (kind: SocialEngagementKind) => {
    if (!session || overrides.like?.pending || overrides.repost?.pending || busy.current === session.did || moderation?.userDid !== session.did) return;
    busy.current = session.did;
    mutation.mutate({ kind, did: session.did, key, existingUri: engagement[kind], uri: post.uri, cid: post.cid, moderation });
  };
  return { engagement, toggleLike: () => toggle("like"), toggleRepost: () => toggle("repost"), isPending: !!(overrides.like?.pending || overrides.repost?.pending) || (mutation.isPending && mutation.variables?.did === session?.did), error: mutation.variables?.did === session?.did ? mutation.error : null };
}
