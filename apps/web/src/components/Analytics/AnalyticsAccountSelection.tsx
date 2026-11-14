"use client";

import { useState } from "react";
import Link from "next/link";
import { ChevronDown } from "lucide-react";
import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { useBrandAccounts } from "@/hooks/useBrandAccounts";
import { FeedHeader } from "@/components/FeedHeader/FeedHeader";
import { AnalyticsAccountDetails } from "./AnalyticsWorkspace";
import { AnalyticsAccountIdentity } from "./AnalyticsAccountIdentity";

export function AnalyticsAccountSelection({ ownerDid }: { ownerDid: string }) {
  const brands = useBrandAccounts(ownerDid);
  const brandsAvailable = brandAccountsEnabled();
  const accounts = [...new Set([ownerDid, ...(brandsAvailable ? brands : [])])];
  const [selected, setSelected] = useState([ownerDid]);
  const [days, setDays] = useState(30);
  const active = brandsAvailable ? selected.filter(did => accounts.includes(did)) : [ownerDid];
  return <div className="flex min-h-0 min-w-0 flex-1 flex-col">
    <FeedHeader title="Analytics" subtitle="Your Content and Engagement">
      <div className="relative"><select aria-label="Analytics Time Range" value={days} onChange={event => setDays(Number(event.target.value))} className="h-9 appearance-none rounded-lg border bg-background py-1 pl-3 pr-9 text-sm"><option value={7}>Last 7 Days</option><option value={30}>Last 30 Days</option><option value={90}>Last 90 Days</option></select><ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-3 size-3" /></div>
    </FeedHeader>
    <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-5"><div className="mx-auto max-w-6xl space-y-6">
      {brandsAvailable ? <fieldset className="space-y-3 rounded-2xl border p-4"><legend className="px-2 font-semibold">Accounts</legend><div className="flex flex-wrap gap-3">{accounts.map(did => <label key={did} className="flex max-w-full cursor-pointer items-center gap-2 rounded-xl border px-3 py-2 text-sm"><input type="checkbox" checked={active.includes(did)} onChange={event => setSelected(previous => event.target.checked ? [...new Set([...previous, did])] : previous.filter(value => value !== did))} /><AnalyticsAccountIdentity did={did} personal={did === ownerDid} /></label>)}</div><Link className="text-xs text-primary hover:underline" href="/brand-accounts">Manage Delegated Accounts</Link></fieldset> : null}
      <SelectedAnalytics accounts={active} ownerDid={ownerDid} days={days} />
    </div></div>
  </div>;
}

function SelectedAnalytics({ accounts, ownerDid, days }: { accounts: string[]; ownerDid: string; days: number }) {
  if (!accounts.length) return <p className="py-12 text-center text-muted-foreground">Select Accounts to View Analytics.</p>;
  return <>
    {accounts.map(did => <section key={did} aria-label={`Analytics for ${did}`} className="rounded-2xl border p-4"><AnalyticsAccountDetails accountDid={did} days={days} personal={did === ownerDid} /></section>)}
  </>;
}
