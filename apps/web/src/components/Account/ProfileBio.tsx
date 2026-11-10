"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AppBskyRichtextFacet } from "@atproto/api";
import { getAtprotoNetwork } from "@/lib/atprotoNetwork";
import { detectProfileBioFacets, resolveProfileBioFacets } from "@/lib/profileBioFacets";
import { SocialRichText } from "@/components/Social/SocialRichText";

export function ProfileBio({ did, text }: { did: string; text: string }) {
  const detected = useMemo(() => detectProfileBioFacets(text), [text]);
  const mentions = detected.some(facet => facet.features.some(AppBskyRichtextFacet.isMention));
  const resolved = useQuery({
    queryKey: ["profileBioFacets", getAtprotoNetwork().publicAppView, did, text],
    queryFn: ({ signal }) => resolveProfileBioFacets(text, signal),
    enabled: mentions,
    staleTime: 5 * 60_000,
    retry: false,
  });
  return <div className="w-full min-w-0 max-w-xl px-4">
    <SocialRichText markdown={false} record={{ $type: "app.bsky.feed.post", text, facets: resolved.data ?? detected, createdAt: "" }} />
  </div>;
}
