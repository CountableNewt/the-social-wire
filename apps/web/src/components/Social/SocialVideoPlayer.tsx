"use client";

import { useEffect, useRef, useState } from "react";
import type Hls from "hls.js";
import type { AppBskyEmbedVideo } from "@atproto/api";
import { loadSocialVideoHls } from "@/lib/socialVideoPlayback";
import { Button } from "@/components/ui/button";
import { SocialAltTextBadge } from "./SocialAltTextBadge";
import { socialHttpsUrl } from "./socialUrls";

export function SocialVideoPlayer({ video, src }: { video: AppBskyEmbedVideo.View; src: string }) {
  const ref = useRef<HTMLVideoElement>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const element = ref.current;
    if (!element) return;
    let disposed = false;
    let started = false;
    let engine: Hls | undefined;
    async function start() {
      if (started || disposed) return;
      started = true;
      try {
        // Shared test providers can supply a direct media file rather than HLS.
        if (/\.(mp4|webm|mov|ogg)$/i.test(new URL(src).pathname)) {
          element!.src = src;
          return;
        }
        const Hls = await loadSocialVideoHls();
        if (disposed) return;
        if (Hls.isSupported()) {
          engine = new Hls({ capLevelToPlayerSize: true, maxBufferLength: 10, maxMaxBufferLength: 20, backBufferLength: 10 });
          engine.on(Hls.Events.ERROR, (_event, data) => {
            if (data.fatal && !disposed) { engine?.destroy(); engine = undefined; setFailed(true); }
          });
          engine.loadSource(src);
          engine.attachMedia(element!);
        } else if (element!.canPlayType("application/vnd.apple.mpegurl")) {
          element!.src = src;
        } else setFailed(true);
      } catch {
        if (!disposed) setFailed(true);
      }
    }
    const observer = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { observer?.disconnect(); void start(); }
    }, { rootMargin: "200px" });
    if (observer) observer.observe(element);
    else void start();
    return () => {
      disposed = true;
      observer?.disconnect();
      engine?.destroy();
      element.removeAttribute("src");
      element.load();
    };
  }, [src, attempt]);
  const ratio = video.aspectRatio;
  const validRatio = ratio && Number.isFinite(ratio.width) && Number.isFinite(ratio.height) && ratio.width > 0 && ratio.height > 0;
  const aspectRatio = validRatio ? `${ratio.width} / ${ratio.height}` : "16 / 9";
  return <div className="flex min-w-0 flex-col gap-2">
    <div className="relative overflow-hidden rounded-xl bg-black" style={{ aspectRatio }}>
    <video ref={ref} controls playsInline preload="metadata" poster={socialHttpsUrl(video.thumbnail)}
      aria-label={video.alt || "Post Video"} width={validRatio ? ratio.width : undefined} height={validRatio ? ratio.height : undefined}
      style={{ aspectRatio }}
      onError={() => setFailed(true)} className="block h-full w-full object-contain" />
    {video.alt ? <SocialAltTextBadge alt={video.alt} label="View Video Alt Text" /> : null}
    {failed ? <div role="status" className="absolute inset-0 flex flex-wrap items-center justify-center gap-3 bg-background/90 p-3 text-sm text-muted-foreground">
      <p>Video Could Not Be Loaded.</p><Button size="sm" variant="outline" onClick={() => { setFailed(false); setAttempt(value => value + 1); }}>Retry Video</Button>
    </div> : null}
    </div>
  </div>;
}
