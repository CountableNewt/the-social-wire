"use client";

import { useState } from "react";
import Link from "next/link";
import { Tabs } from "@base-ui/react/tabs";
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
  const [selected, setSelected] = useState(ownerDid);
  const [days, setDays] = useState(30);
  const active = accounts.includes(selected) ? selected : ownerDid;
  return <div className="flex min-h-0 min-w-0 flex-1 flex-col">
    <FeedHeader title="Analytics" subtitle="Your Content and Engagement">
      <div className="relative"><select aria-label="Analytics Time Range" value={days} onChange={event => setDays(Number(event.target.value))} className="h-9 appearance-none rounded-lg border bg-background py-1 pl-3 pr-9 text-sm"><option value={7}>Last 7 Days</option><option value={30}>Last 30 Days</option><option value={90}>Last 90 Days</option></select><ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-3 size-3" /></div>
    </FeedHeader>
    <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-5"><div className="mx-auto max-w-6xl space-y-6">
      {brandsAvailable ? (
        <Tabs.Root
          className="space-y-4"
          orientation="horizontal"
          value={active}
          onValueChange={value => { if (typeof value === "string") setSelected(value); }}
        >
          <Tabs.List aria-label="Analytics Accounts" activateOnFocus className="flex gap-1 overflow-x-auto border-b p-1">
            {accounts.map(did => (
              <Tabs.Tab
                key={did}
                value={did}
                className="max-w-56 shrink-0 cursor-pointer rounded-md border-b-2 border-transparent px-3 py-2 text-left text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring data-active:border-primary data-active:bg-muted data-active:text-foreground"
              >
                <span className="block truncate"><AnalyticsAccountIdentity did={did} personal={did === ownerDid} /></span>
              </Tabs.Tab>
            ))}
          </Tabs.List>
          <Link className="inline-block text-xs text-primary hover:underline" href="/brand-accounts">Manage Delegated Accounts</Link>
          {accounts.map(did => (
            <Tabs.Panel key={did} value={did} className="rounded-2xl border p-4">
              <AnalyticsAccountDetails accountDid={did} days={days} personal={did === ownerDid} />
            </Tabs.Panel>
          ))}
        </Tabs.Root>
      ) : (
        <section aria-label={`Analytics for ${ownerDid}`} className="rounded-2xl border p-4">
          <AnalyticsAccountDetails accountDid={ownerDid} days={days} personal />
        </section>
      )}
    </div></div>
  </div>;
}
