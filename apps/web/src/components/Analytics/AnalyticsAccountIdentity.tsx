"use client";

import { useEffect, useState } from "react";
import { Agent } from "@atproto/api";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { Avatar } from "@/components/shared/Avatar";

/** Labels are public account metadata; account OAuth credentials are never sent here. */
export function AnalyticsAccountIdentity({ did, personal = false, presentation = "label" }: { did: string; personal?: boolean; presentation?: "label" | "header" }) {
  const [profile, setProfile] = useState<{ did: string; handle: string; name?: string; avatar?: string }>();
  useEffect(() => {
    const controller = new AbortController();
    const agent = new Agent(getAtprotoNetwork().publicAppView);
    void agent.getProfile({ actor: did }, { signal: controller.signal }).then(({ data }) => {
      if (!controller.signal.aborted && data.did === did) setProfile({ did, handle: data.handle, name: data.displayName, avatar: data.avatar });
    }).catch(() => { /* DID remains a useful identity when metadata is unavailable. */ });
    return () => controller.abort();
  }, [did]);
  const current = profile?.did === did ? profile : undefined;
  if (presentation === "header") {
    const name = current?.name?.trim() || current?.handle || (personal ? "Personal Account" : did);
    return <div className="flex min-w-0 items-center gap-2.5" title={did}>
      <Avatar src={current?.avatar} alt={name} size={32} className="shrink-0" />
      <div className="min-w-0 text-left">
        <h2 className="line-clamp-2 break-words text-base font-medium leading-tight">{name}</h2>
        <p className="truncate text-sm leading-snug text-muted-foreground">{current?.handle || did}</p>
      </div>
    </div>;
  }
  return <span className="min-w-0 break-all" title={did}>{current ? `${current.name || current.handle} (@${current.handle})` : personal ? "Personal Account" : did}{current && personal ? " · Personal" : ""}</span>;
}
