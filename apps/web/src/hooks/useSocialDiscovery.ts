"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import type { ModerationOpts } from "@atproto/api";
import { useAuth } from "./useAuth";
import { getSocialExplorePage, getSocialNotificationsPage, getSocialNotificationUnread, markSocialNotificationsSeen, nextDiscoveryCursor, type SocialExploreKind, type SocialNotificationPage } from "@/lib/blueskySocialDiscoveryClient";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";

export type SocialExploreActor = {
  ownerDid: string;
  session: OAuthSession;
  assertCurrentActor: () => void;
};

export function useSocialExplore(query: string, kind: SocialExploreKind, moderation?: ModerationOpts, actor?: SocialExploreActor) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const did = actor?.session.did ?? session?.did;
  const scope = actor ? ["brandAccounts", actor.ownerDid, did] : ["blueskySocial", did ?? ""];
  return useInfiniteQuery({
    queryKey: [...scope, "explore", oauthSessionReloadSeq, query.trim(), kind, moderation?.prefs.labelers.map(labeler => labeler.did)],
    enabled: !!did && moderation?.userDid === did,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam, signal }) => {
      const oauth = actor?.session ?? getOAuthSession();
      if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
      if (oauth.did !== did || moderation?.userDid !== oauth.did) throw new Error("Your account changed. Please reload this page.");
      actor?.assertCurrentActor();
      const result = await getSocialExplorePage({ session: oauth, moderation, query, kind, cursor: pageParam, signal });
      actor?.assertCurrentActor();
      return result;
    },
    getNextPageParam: (page, _pages, _last, pageParams) => nextDiscoveryCursor(page, pageParams),
    staleTime: 30_000,
    retry: false,
  });
}

export function useSocialNotifications(moderation?: ModerationOpts) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  return useInfiniteQuery({
    queryKey: ["blueskySocial", session?.did ?? "", "notifications", oauthSessionReloadSeq, moderation?.prefs.labelers.map(labeler => labeler.did)],
    enabled: !!session && moderation?.userDid === session.did,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => {
      const oauth = getOAuthSession();
      if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
      if (oauth.did !== session?.did || moderation?.userDid !== oauth.did) throw new Error("Your account changed. Please reload this page.");
      return getSocialNotificationsPage({ session: oauth, moderation, cursor: pageParam, signal });
    },
    getNextPageParam: (page, _pages, _last, pageParams) => nextDiscoveryCursor(page, pageParams),
    staleTime: 30_000,
    retry: false,
  });
}

export function useSocialNotificationUnread(enabled: boolean, refetchInterval?: number) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  return useQuery({
    queryKey: ["blueskySocial", session?.did ?? "", "notificationUnread", oauthSessionReloadSeq],
    enabled: enabled && !!session,
    queryFn: ({ signal }) => {
      const oauth = getOAuthSession();
      if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
      if (oauth.did !== session?.did) throw new Error("Your account changed. Please reload this page.");
      return getSocialNotificationUnread(oauth, signal);
    },
    staleTime: 15_000,
    refetchInterval,
    retry: false,
  });
}

export function useMarkSocialNotificationsSeen() {
  const { session, getOAuthSession } = useAuth();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (seenAt: string = new Date().toISOString()) => {
      const oauth = getOAuthSession();
      if (!oauth) throw new Error(SCOPE_RECOVERY_MESSAGE);
      if (oauth.did !== session?.did) throw new Error("Your account changed. Please reload this page.");
      await markSocialNotificationsSeen(oauth, seenAt);
      return { did: oauth.did, seenAt };
    },
    onSuccess: async ({ did, seenAt }) => {
      queryClient.setQueriesData<InfiniteData<SocialNotificationPage>>({ queryKey: ["blueskySocial", did, "notifications"] }, current => current ? { ...current, pages: current.pages.map(page => ({ ...page, notifications: page.notifications.map(notification => Date.parse(notification.indexedAt) < Date.parse(seenAt) ? { ...notification, isRead: true } : notification) })) } : current);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["blueskySocial", did, "notificationUnread"] }),
        queryClient.invalidateQueries({ queryKey: ["blueskySocial", did, "notifications"] }),
      ]);
    },
  });
}
