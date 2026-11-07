"use client";
/* eslint-disable @next/next/no-img-element -- Preserve publisher image formats. */
import { memo, useContext, useEffect, useRef, useState } from "react";
import type { AppBskyEmbedExternal } from "@atproto/api";
import { socialHttpsUrl } from "./socialUrls";
import { SocialStandardSiteEmbed } from "./SocialStandardSiteEmbed";
import { SocialFeedViewportContext } from "./SocialFeedViewportContext";
import { SocialFeedViewportBoundary } from "./SocialFeedViewportBoundary";

export const SocialExternalEmbed = memo(function SocialExternalEmbed({ external, href }: { external: AppBskyEmbedExternal.ViewExternal; href: string }) {
  const container = useRef<HTMLDivElement>(null);
  const viewport = useContext(SocialFeedViewportContext);
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    if (!container.current) return;
    if (typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) {
        setVisible(true);
        observer.disconnect();
      }
    }, { rootMargin: "200px" });
    observer.observe(container.current);
    return () => observer.disconnect();
  }, []);
  const thumb = socialHttpsUrl(external.thumb);
  const fallback = <a href={href} target="_blank" rel="noopener noreferrer" className="block overflow-hidden rounded-xl border bg-muted/30 hover:bg-muted/60">
    {thumb ? <div data-social-feed-section={JSON.stringify([href, "preview-image"])} className="relative h-48 w-full bg-muted sm:h-60"><img src={thumb} alt="" loading="lazy" referrerPolicy="no-referrer" className="absolute inset-0 h-full w-full object-contain" /></div> : null}
    <div data-social-feed-section={JSON.stringify([href, "preview-body"])} className="flex flex-col gap-1 p-3">
      <p className="text-sm font-medium">{external.title}</p>
      <p className="line-clamp-3 text-sm text-muted-foreground">{external.description}</p>
      <p className="text-xs text-muted-foreground">{new URL(href).hostname}</p>
    </div>
  </a>;
  const content = <div ref={container}>{visible ? <SocialStandardSiteEmbed key={href} external={external} href={href} fallback={fallback} /> : fallback}</div>;
  // The near-viewport switch can immediately reveal already-cached metadata.
  return viewport ? <SocialFeedViewportBoundary {...viewport}>{content}</SocialFeedViewportBoundary> : content;
});
