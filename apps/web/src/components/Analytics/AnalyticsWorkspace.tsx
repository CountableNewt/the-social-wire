"use client";

import { RefreshCw } from "lucide-react";
import { useAuth } from "@/hooks/useAuth";
import { useCreatorAnalytics } from "@/hooks/useCreatorAnalytics";
import { SocialPermissionRecovery } from "@/components/Social/SocialPermissionRecovery";
import { Button } from "@/components/ui/button";
import { AnalyticsAccountSelection } from "./AnalyticsAccountSelection";
import { BlueskyAnalytics } from "./BlueskyAnalytics";
import { AnalyticsAccountIdentity } from "./AnalyticsAccountIdentity";

export function AnalyticsWorkspace() {
  const { session } = useAuth();
  return session ? <AnalyticsAccountSelection key={session.did} ownerDid={session.did} /> : <p className="p-6">Log In to View Analytics.</p>;
}

export function AnalyticsAccountDetails({ accountDid, days, personal = false }: { accountDid: string; days: number; personal?: boolean }) {
  const { blueskyQuery, refresh, collect } = useCreatorAnalytics(days, accountDid);
  const refreshError = refresh.error instanceof Error ? refresh.error.message : refresh.error ? "Analytics could not be refreshed. Please try again." : undefined;
  return <div className="space-y-4">
    <div className="flex min-w-0 items-center justify-between gap-3"><AnalyticsAccountIdentity did={accountDid} personal={personal} presentation="header" /><Button className="shrink-0" size="sm" variant="outline" disabled={refresh.isPending || blueskyQuery.isFetching} onClick={() => { void collect().catch(() => {}); }}><RefreshCw className={`size-4 ${refresh.isPending ? "animate-spin" : ""}`} />{refresh.isPending ? "Refreshing…" : "Refresh"}</Button></div>
    {refreshError ? <div role="alert" className="rounded-xl border border-destructive/40 p-4 text-sm"><p>{refreshError}</p>{/log in again/i.test(refreshError) ? <SocialPermissionRecovery /> : null}</div> : null}
    <BlueskyAnalytics data={blueskyQuery.data} isPending={blueskyQuery.isPending} error={blueskyQuery.error} retry={() => { void blueskyQuery.refetch(); }} />
  </div>;
}
