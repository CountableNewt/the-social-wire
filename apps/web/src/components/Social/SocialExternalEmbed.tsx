"use client";
/* eslint-disable @next/next/no-img-element -- Preserve publisher image formats. */

import { memo, useContext } from "react";
import type { AppBskyEmbedExternal } from "@atproto/api";
import { socialHttpsUrl } from "./socialUrls";
import { SocialFeedViewportContext } from "./SocialFeedViewportContext";
import { SocialFeedViewportBoundary } from "./SocialFeedViewportBoundary";

export const SocialExternalEmbed = memo(function SocialExternalEmbed({ external, href }: { external: AppBskyEmbedExternal.ViewExternal; href: string }) {
  const viewport = useContext(SocialFeedViewportContext);
  const thumb = socialHttpsUrl(external.thumb);
  const content = <a href={href} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-xl border bg-muted/30 hover:bg-muted/60">
    {thumb ? <div data-social-feed-section={JSON.stringify([href, "preview-image"])} className="relative h-48 w-full bg-muted sm:h-60"><img src={thumb} alt="" loading="lazy" referrerPolicy="no-referrer" className="absolute inset-0 h-full w-full object-contain" /></div> : null}
    <div data-social-feed-section={JSON.stringify([href, "preview-body"])} className="flex flex-col gap-1 p-3">
      <p className="text-sm font-medium">{external.title}</p>
      <p className="line-clamp-3 text-sm text-muted-foreground">{external.description}</p>
      <p className="text-xs text-muted-foreground">{new URL(href).hostname}</p>
    </div>
  </a>;
  return viewport ? <SocialFeedViewportBoundary {...viewport}>{content}</SocialFeedViewportBoundary> : content;
});
