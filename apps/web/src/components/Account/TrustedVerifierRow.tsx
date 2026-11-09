"use client";

import { useQuery } from "@tanstack/react-query";
import { resolveVerifierProfile } from "@/lib/socialVerification";
import { Avatar } from "@/components/shared/Avatar";
import { Button } from "@/components/ui/button";

export function TrustedVerifierRow({ did, disabled, onRemove }: { did: string; disabled: boolean; onRemove: () => void }) {
  const { data } = useQuery({
    queryKey: ["verificationIssuerProfile", did],
    queryFn: async ({ signal }) => {
      const profile = await resolveVerifierProfile(did, signal);
      if (profile.did !== did) throw new Error("The Verifier Profile Did Not Match Its Identity.");
      return profile;
    },
    staleTime: 5 * 60_000,
    retry: false,
  });
  const profile = data?.did === did ? data : undefined;
  const name = profile?.displayName?.trim() || profile?.handle || did;
  const handle = profile?.handle ? `@${profile.handle}` : undefined;
  return <li className="flex min-w-0 items-center justify-between gap-3 rounded-lg border p-3">
    <div className="flex min-w-0 flex-1 items-center gap-3" title={did}>
      <Avatar src={profile?.avatar} alt={name} size={32} className="shrink-0" />
      <div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{name}</p>{handle ? <p className="truncate text-xs text-muted-foreground">{handle}</p> : null}</div>
    </div>
    <Button type="button" variant="ghost" size="sm" className="shrink-0" disabled={disabled} aria-label={`Remove Verifier: ${handle ?? did}`} onClick={onRemove}>Remove</Button>
  </li>;
}
