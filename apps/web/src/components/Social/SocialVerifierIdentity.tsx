"use client";

import { useQuery } from "@tanstack/react-query";
import { resolveVerifierProfile } from "@/lib/socialVerification";

export function SocialVerifierIdentity({ did }: { did: string }) {
  const { data } = useQuery({
    queryKey: ["verificationIssuerProfile", did],
    queryFn: ({ signal }) => resolveVerifierProfile(did, signal),
    staleTime: 5 * 60_000,
    retry: false,
  });
  const profile = data?.did === did ? data : undefined;
  return <li className="break-words [overflow-wrap:anywhere]">
    {profile ? <>{profile.displayName ? `${profile.displayName} ` : ""}@{profile.handle}</> : did}
  </li>;
}
