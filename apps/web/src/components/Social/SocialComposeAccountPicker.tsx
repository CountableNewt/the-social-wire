"use client";

import { brandAccountsEnabled } from "@/lib/brandAccountsEnabled";
import { useId } from "react";
import { useQueries } from "@tanstack/react-query";
import { ChevronDown } from "lucide-react";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { getBrandAccountPublicProfile } from "@/lib/brandAccounts/brandAccountPublicProfile";
import { Avatar } from "@/components/shared/Avatar";

type AccountPickerProps = { label?: string; ownerDid: string; accounts: string[]; actorDid: string; disabled: boolean; onChange: (did: string) => void };

export function SocialComposeAccountPicker(props: AccountPickerProps) {
  return brandAccountsEnabled() ? <EnabledAccountPicker {...props} /> : null;
}

function EnabledAccountPicker({ ownerDid, accounts, actorDid, disabled, onChange, label = "Post As" }: AccountPickerProps) {
  const fieldId = useId();
  const appView = getAtprotoNetwork().publicAppView;
  const dids = [ownerDid, ...accounts.filter(did => did !== ownerDid)];
  const queries = useQueries({ queries: dids.map(did => ({ queryKey: ["brandPublicProfile", did, appView], queryFn: ({ signal }: { signal: AbortSignal }) => getBrandAccountPublicProfile(did, appView, signal), staleTime: 300_000, retry: false })) });
  const identities = dids.map((did, index) => { const query = queries[index]; return !query.isError && query.data?.did === did ? query.data : undefined; });
  const selected = identities[dids.indexOf(actorDid)];
  return <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-x-2 gap-y-1">
    <label htmlFor={fieldId} className="col-start-2 row-start-1 ps-[calc(--spacing(3)+1px)] text-xs text-muted-foreground">{label}</label>
    <Avatar src={selected?.avatar} alt="" size={36} className="col-start-1 row-start-2" />
    <div className="relative col-start-2 row-start-2">
      <select id={fieldId} value={actorDid} disabled={disabled} onChange={event => onChange(event.target.value)} className="block w-full min-w-0 appearance-none truncate rounded-xl border bg-background py-2 pl-3 pr-9 text-sm">
        {dids.map((did, index) => {
          const identity = identities[index];
          const optionLabel = identity
            ? [identity.displayName, `@${identity.handle}`].filter(Boolean).join(" ")
            : did === ownerDid ? "Personal Account" : `Linked Delegated Account ${index}`;
          return <option key={did} value={did}>{optionLabel}</option>;
        })}
        {!dids.includes(actorDid) ? <option value={actorDid}>Unlinked Delegated Account</option> : null}
      </select>
      <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 size-4 -translate-y-1/2" />
    </div>
  </div>;
}
