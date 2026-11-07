"use client";

import { useEffect, useRef } from "react";
import type { ComposerVideo } from "@/lib/composerMedia";

export function SocialVideoPreview({ video }: { video: ComposerVideo }) {
  const player = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    if (!video.blob) return;
    const url = URL.createObjectURL(video.blob);
    const element = player.current;
    if (element) element.src = url;
    return () => { if (element) { element.pause(); element.removeAttribute("src"); element.load(); } URL.revokeObjectURL(url); };
  }, [video.blob]);
  if (!video.blob) return <div className="flex h-40 items-center justify-center rounded-lg bg-muted text-sm">{video.status === "failed" ? "Video Unavailable" : "Preparing Video…"}</div>;
  return <video ref={player} controls playsInline preload="metadata" aria-label={video.alt || `Preview ${video.name}`} className="max-h-80 w-full rounded-lg bg-black object-contain" />;
}
