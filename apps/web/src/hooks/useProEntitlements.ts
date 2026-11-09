"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/hooks/useAuth";
import { getProEntitlements, isProGatingEnabled } from "@/lib/proEntitlements";

export function useProEntitlements(enabled = true) {
  const { session, getOAuthSession, oauthSessionReloadSeq } = useAuth();
  const did = session?.did;
  const gatingEnabled = isProGatingEnabled();
  const query = useQuery({
    queryKey: ["proEntitlements", did, oauthSessionReloadSeq],
    enabled: enabled && !!did && gatingEnabled,
    staleTime: 0,
    refetchInterval: enabled ? 60_000 : false,
    retry: false,
    queryFn: ({ signal }) => {
      const oauth = getOAuthSession();
      if (!oauth || oauth.did !== did) throw new Error("Your Account Changed. Retry Pro Access.");
      return getProEntitlements(oauth, signal);
    },
  });
  // isPro represents feature access; disabling enforcement does not change the viewer's plan.
  return { ...query, refetch: (options?: Parameters<typeof query.refetch>[0]) => enabled ? query.refetch(options) : Promise.resolve(query), isPro: enabled && !!did && (!gatingEnabled || (!query.isError && query.data?.viewerDid === did && query.data.plan === "pro")) };
}
