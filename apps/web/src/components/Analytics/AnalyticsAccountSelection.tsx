"use client";

import { useState } from "react";
import Link from "next/link";
import { ChevronDown } from "lucide-react";
import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { useBrandAccounts } from "@/hooks/useBrandAccounts";
import { FeedHeader } from "@/components/FeedHeader/FeedHeader";
import { AnalyticsAccountDetails } from "./AnalyticsWorkspace";
import { AnalyticsAccountIdentity } from "./AnalyticsAccountIdentity";
import { floatingSidebarClassName, floatingSidebarRowClassName } from "@/components/shared/floatingSidebarStyles";

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
    <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-5"><div className={`mx-auto grid max-w-6xl min-w-0 gap-5 ${brandsAvailable ? "lg:grid-cols-[15rem_minmax(0,1fr)]" : ""}`}>
      {brandsAvailable ? <aside aria-label="Analytics Accounts" className={`${floatingSidebarClassName} w-full space-y-3 lg:sticky lg:top-0 lg:max-h-[calc(100svh-10rem)] lg:overflow-y-auto lg:overscroll-contain`}>
        <h2 className="px-2 text-xs font-medium text-muted-foreground">Accounts</h2>
        <div role="tablist" aria-label="Analytics Accounts" aria-orientation="vertical" className="space-y-1">
          {accounts.map((did, index) => <button key={did} type="button" role="tab" id={`analytics-tab-${did}`} aria-controls={`analytics-panel-${did}`} aria-selected={active === did} tabIndex={active === did ? 0 : -1}
            onClick={() => setSelected(did)}
            onKeyDown={event => {
              const next = event.key === "Home" ? 0 : event.key === "End" ? accounts.length - 1 : event.key === "ArrowDown" ? (index + 1) % accounts.length : event.key === "ArrowUp" ? (index - 1 + accounts.length) % accounts.length : undefined;
              if (next === undefined) return;
              event.preventDefault();
              setSelected(accounts[next]);
              document.getElementById(`analytics-tab-${accounts[next]}`)?.focus();
            }}
            className={`${floatingSidebarRowClassName} w-full text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring ${active === did ? "bg-accent font-medium text-accent-foreground" : "hover:bg-accent/60"}`}>
            <AnalyticsAccountIdentity did={did} personal={did === ownerDid} />
          </button>)}
        </div>
        <Link className="block px-2 text-xs text-primary hover:underline" href="/brand-accounts">Manage Delegated Accounts</Link>
      </aside> : null}
      <section key={active} id={`analytics-panel-${active}`} role={brandsAvailable ? "tabpanel" : "region"} aria-labelledby={brandsAvailable ? `analytics-tab-${active}` : undefined} aria-label={brandsAvailable ? undefined : `Analytics for ${active}`} tabIndex={brandsAvailable ? 0 : undefined} className="min-w-0 rounded-2xl border p-4">
        <AnalyticsAccountDetails accountDid={active} days={days} personal={active === ownerDid} />
      </section>
    </div></div>
  </div>;
}
