"use client";

import { useQuery } from "@tanstack/react-query";
import { useAuth } from "@/hooks/useAuth";
import { listMarginAnnotations } from "@/lib/research/marginResearchClient";

export function useMarginAnnotations(did: string, enabled: boolean) {
  const { getOAuthSession, oauthSessionReloadSeq } = useAuth();
  return useQuery({
    queryKey: ["research", did, "margin", oauthSessionReloadSeq],
    queryFn: ({ signal }) => {
      const session = getOAuthSession();
      if (!session || session.did !== did) throw new Error("Log In Again to Load Research.");
      return listMarginAnnotations(session, signal);
    },
    enabled,
    staleTime: 60_000,
    retry: false,
  });
}
