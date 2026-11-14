"use client";

import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { useCallback, useState } from "react";
import { useBrandAccounts } from "@/hooks/useBrandAccounts";
import { includeBrandNotifications, setIncludeBrandNotifications } from "@/lib/brandAccounts/brandNotificationPreferences";
import { BrandAccountViewer, type DelegatedNotificationSource, type ReportDelegatedNotifications } from "@/components/BrandAccounts/BrandAccountViewer";
import { moderateNotification } from "@atproto/api";
import { useQueryClient } from "@tanstack/react-query";
import { FeedHeader } from "@/components/FeedHeader/FeedHeader";
import { Button } from "@/components/ui/button";
import { useAuth } from "@/hooks/useAuth";
import { usePageReadDelay } from "@/hooks/usePageReadDelay";
import { useBlueskySocialCatalog } from "@/hooks/useBlueskySocialCatalog";
import { useSocialNotifications, useSocialNotificationUnread, useMarkSocialNotificationsSeen } from "@/hooks/useSocialDiscovery";
import { notificationFeedItem, notificationIdentity, notificationPostUri, notificationReadBoundary, socialDiscoveryErrorMessage } from "@/lib/blueskySocialDiscoveryClient";
import { SCOPE_RECOVERY_MESSAGE } from "@/lib/oauthScopeRecovery";
import { SocialPostLink } from "./SocialPostLink";
import { InteractiveSocialPost } from "./InteractiveSocialPost";
import { SocialPermissionRecovery } from "./SocialPermissionRecovery";
import { SocialTimelineSkeleton } from "./SocialTimelineSkeleton";
import { moderateSocialPost } from "./socialModeration";
import { groupSocialNotifications } from "@/lib/socialNotificationGrouping";
import { SocialNotificationGroupHeader } from "./SocialNotificationGroupHeader";
import { isSocialInteractionHidden } from "@/lib/socialInteractionModeration";

export function SocialNotifications() {
  const { session } = useAuth();
  return <SocialNotificationsViewer key={session?.did ?? "signed-out"} />;
}

function SocialNotificationsViewer() {
  const { session } = useAuth();
  const linkedAccounts = useBrandAccounts(session?.did ?? "");
  const brandsAvailable = brandAccountsEnabled();
  const brandAccounts = brandsAvailable ? linkedAccounts : [];
  const [includeBrands, setIncludeBrands] = useState(() => brandsAvailable && includeBrandNotifications(session?.did ?? ""));
  const [delegatedSources, setDelegatedSources] = useState<Record<string, DelegatedNotificationSource>>({});
  const reportNotifications = useCallback<ReportDelegatedNotifications>((did, source) => {
    setDelegatedSources(previous => {
      if (source) return { ...previous, [did]: source };
      const next = { ...previous };
      delete next[did];
      return next;
    });
  }, []);
  function changeIncludeBrands(value: boolean) {
    if (!brandAccountsEnabled()) return;
    setIncludeBrands(value);
    if (session) setIncludeBrandNotifications(session.did, value);
  }
  const catalog = useBlueskySocialCatalog();
  const availableModeration = catalog.data?.moderation;
  const moderation = !catalog.isError && availableModeration?.userDid === session?.did ? availableModeration : undefined;
  const notifications = useSocialNotifications(moderation);
  const unread = useSocialNotificationUnread(!!moderation);
  const markSeen = useMarkSocialNotificationsSeen();
  const queryClient = useQueryClient();
  const error = catalog.isError ? socialDiscoveryErrorMessage(catalog.error, "Notifications") : notifications.isError ? socialDiscoveryErrorMessage(notifications.error, "Notifications") : undefined;
  const pages = notifications.data?.pages ?? [];
  const readBoundary = notificationReadBoundary(pages.flatMap(page => page.notifications));
  usePageReadDelay(session && readBoundary ? `${session.did}:${readBoundary}` : undefined,
    !!moderation && !!notifications.data && !error && !markSeen.isPending && !markSeen.isError,
    () => { if (readBoundary) markSeen.mutate(readBoundary); });
  const posts = new Map(pages.flatMap(page => page.posts).map(post => [post.uri, post]));
  const unavailablePosts = new Set(pages.flatMap(page => page.unavailablePostUris ?? []));
  const seen = new Set<string>();
  const rows = moderation ? pages.flatMap(page => page.notifications).filter(notification => {
    const key = notificationIdentity(notification);
    const decision = moderateNotification(notification, moderation).ui("contentList");
    const post = posts.get(notificationPostUri(notification) ?? "");
    const interaction = notificationFeedItem(notification, posts, unavailablePosts);
    if (interaction && isSocialInteractionHidden(interaction, moderation)) return false;
    if (seen.has(key) || decision.filter || decision.blur || decision.noOverride || (post && moderateSocialPost(post, moderation).ui("contentList").filter)) return false;
    seen.add(key); return true;
  }) : [];
  const groups = groupSocialNotifications(rows);
  async function refresh() {
    const settings = await catalog.refetch();
    if (!settings.isError && session) await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["blueskySocial", session.did, "notifications"] }),
      queryClient.invalidateQueries({ queryKey: ["blueskySocial", session.did, "notificationUnread"] }),
    ]);
  }

  const activeBrands = includeBrands ? brandAccounts : [];
  const sources = activeBrands.flatMap(did => delegatedSources[did] ? [delegatedSources[did]] : []);
  const showRecipients = activeBrands.length > 0;
  const personalRows = moderation ? groups.map(group => {
    const uri = notificationPostUri(group.latest);
    const item = notificationFeedItem(group.latest, posts, unavailablePosts);
    return {
      group,
      content: <>
        <SocialNotificationGroupHeader group={group} moderation={moderation} />
        {item ? <InteractiveSocialPost item={item} moderation={moderation} showReplyParentActions /> : uri ? <SocialPostLink uri={uri} moderation={moderation} /> : null}
      </>,
      recipient: showRecipients ? "Personal Account" : undefined,
      key: `${session?.did}:${group.key}`,
    };
  }) : [];
  const combinedRows = [
    ...(error ? [] : personalRows),
    ...activeBrands.flatMap(did => {
      const source = delegatedSources[did];
      return (source?.rows ?? []).map(row => ({
        ...row,
        recipient: source.recipient,
        key: `${did}:${row.group.key}`,
      }));
    }),
  ];
  if (showRecipients) combinedRows.sort((a, b) => {
    const aTime = Date.parse(a.group.latest.indexedAt) || 0;
    const bTime = Date.parse(b.group.latest.indexedAt) || 0;
    return bTime - aTime;
  });

  const allUnreadKnown = !unread.isError && unread.data !== undefined &&
    activeBrands.every(did => delegatedSources[did]?.unread !== undefined);
  const unreadSubtitle = allUnreadKnown
    ? `${unread.data! + sources.reduce((sum, source) => sum + (source.unread ?? 0), 0)} Unread`
    : unread.isError ? "Unread Counts Unavailable"
    : unread.data === undefined ? "Social"
    : `${unread.data} Personal Unread · Delegated Counts Unavailable`;
  const personalLoading = !error && (!moderation || notifications.isPending);
  const loading = personalLoading || sources.length < activeBrands.length || sources.some(source => source.loading);
  const paging = notifications.isFetchingNextPage || sources.some(source => source.paging);
  const hasNextPage = (!error && notifications.hasNextPage) || sources.some(source => source.loadMore);
  const canMarkPersonal = !!moderation && !markSeen.isPending && !notifications.isPending && !error;

  function markAllSeen() {
    const seenAt = new Date().toISOString();
    if (canMarkPersonal) markSeen.mutate(seenAt);
    for (const source of sources) source.markSeen?.(seenAt);
  }

  function refreshAll() {
    void refresh();
    for (const source of sources) source.refresh();
  }

  function loadMore() {
    if (!error && notifications.hasNextPage) void notifications.fetchNextPage();
    for (const source of sources) source.loadMore?.();
  }

  return <div className="flex min-h-0 w-full flex-1 flex-col">
    <FeedHeader title="Notifications" subtitle={!moderation ? "Social" : unreadSubtitle}>
      <Button size="sm" variant="ghost" disabled={markSeen.isPending || (!canMarkPersonal && !sources.some(source => source.markSeen))} onClick={markAllSeen}>{markSeen.isPending ? "Marking…" : "Mark All As Read"}</Button>
      <Button size="sm" variant="ghost" disabled={catalog.isFetching || notifications.isFetching || sources.some(source => source.refreshing)} onClick={refreshAll}>Refresh</Button>
    </FeedHeader>
    <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain" key={session?.did}>
      <div className="mx-auto flex max-w-2xl flex-col gap-4 p-3 sm:p-4">
        {session && brandAccounts.length ? <label className="flex cursor-pointer items-center gap-2 text-sm">
          <input type="checkbox" checked={includeBrands} onChange={event => changeIncludeBrands(event.target.checked)} />
          Include Delegated Accounts
        </label> : null}
        {session ? activeBrands.map(did => <BrandAccountViewer key={did} ownerDid={session.did} brandDid={did} reportNotifications={reportNotifications} />) : null}
        {activeBrands.map(did => <div key={did} className="contents">{delegatedSources[did]?.status}</div>)}
        {markSeen.isError || unread.isError ? <section role="alert" className="flex flex-col items-start gap-2"><p className="text-sm text-destructive">{socialDiscoveryErrorMessage(markSeen.error ?? unread.error, "Notifications")}</p>{socialDiscoveryErrorMessage(markSeen.error ?? unread.error, "Notifications") === SCOPE_RECOVERY_MESSAGE ? <SocialPermissionRecovery /> : <Button variant="outline" size="sm" onClick={() => { if (markSeen.isError) markSeen.mutate(new Date().toISOString()); else void unread.refetch(); }}>Retry Unread Status</Button>}</section> : null}
        {error ? <section role="alert" className="flex flex-col items-start gap-3 py-4">
          <h2 className="text-lg font-semibold">Notifications Unavailable</h2><p className="text-sm text-muted-foreground">{error}</p>
          {error === SCOPE_RECOVERY_MESSAGE ? <SocialPermissionRecovery /> : <Button variant="outline" onClick={() => { void refresh(); }}>Retry</Button>}
        </section> : null}
        {combinedRows.map(({ group, content, recipient, key }) => (
          <section key={key} aria-label={`Notification: ${group.latest.reason}`} className="flex flex-col gap-3 rounded-2xl border bg-card p-4">
            {recipient ? <p className="break-all text-xs font-medium text-muted-foreground">For {recipient}</p> : null}
            {content}
          </section>
        ))}
        {loading ? <SocialTimelineSkeleton /> : null}
        {!loading && !error && !combinedRows.length && !sources.some(source => source.status) ? <p className="py-8 text-center text-sm text-muted-foreground">No notifications to show.</p> : null}
        {hasNextPage ? <Button variant="outline" disabled={paging} onClick={loadMore}>{paging ? "Loading…" : "Load More"}</Button> : null}
      </div>
    </div>
  </div>;
}
