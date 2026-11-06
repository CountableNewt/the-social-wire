"use client";

/* eslint-disable @next/next/no-img-element -- GIF posters retain provider formats. */
import { useCallback, useEffect, useRef, useState } from "react";
import type { GifRendition } from "@/lib/composerMedia";
import { Button } from "@/components/ui/button";

export function SocialGifPlayer({ source, poster, mp4, webm, alt, width, height, files, onOpenViewer }: { source: string; poster?: string; mp4?: string; webm?: string; alt: string; width?: number; height?: number; files?: Partial<Record<GifRendition, Blob>>; onOpenViewer?: () => void }) {
  const [playing, setPlaying] = useState(true);
  const [error, setError] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const container = useRef<HTMLDivElement>(null);
  const image = useRef<HTMLImageElement>(null);
  const mp4Source = useRef<HTMLSourceElement>(null);
  const webmSource = useRef<HTMLSourceElement>(null);
  const urls = useRef<Partial<Record<GifRendition, string>>>({});
  const manuallyPaused = useRef(false);
  const playbackRequest = useRef(0);
  const gifBlob = files?.gif, posterBlob = files?.poster, mp4Blob = files?.mp4, webmBlob = files?.webm;
  useEffect(() => {
    const local: Partial<Record<GifRendition, string>> = {};
    for (const [rendition, blob] of [["gif", gifBlob], ["poster", posterBlob], ["mp4", mp4Blob], ["webm", webmBlob]] as const) if (blob) local[rendition] = URL.createObjectURL(blob);
    urls.current = local;
    if (video.current && local.poster) video.current.poster = local.poster;
    if (mp4Source.current && local.mp4) mp4Source.current.src = local.mp4;
    if (webmSource.current && local.webm) webmSource.current.src = local.webm;
    if (image.current && local.poster) image.current.src = local.poster;
    if (video.current && (local.mp4 || local.webm)) video.current.load();
    return () => { Object.values(local).forEach(url => URL.revokeObjectURL(url)); urls.current = {}; };
  }, [gifBlob, posterBlob, mp4Blob, webmBlob]);
  useEffect(() => {
    const uri = playing ? urls.current.gif : urls.current.poster;
    if (image.current && uri) image.current.src = uri;
  }, [playing, gifBlob, posterBlob]);
  const cancelPlayback = useCallback(() => { playbackRequest.current++; }, []);
  const pause = useCallback(() => { cancelPlayback(); video.current?.pause(); setPlaying(false); }, [cancelPlayback]);
  const play = useCallback(async (manual = false) => {
    const request = ++playbackRequest.current;
    try {
      if (video.current) await video.current.play();
      if (request === playbackRequest.current) setPlaying(true);
    } catch {
      if (request === playbackRequest.current) { setPlaying(false); if (manual) setError(true); }
    }
  }, []);
  useEffect(() => {
    const element = container.current;
    const media = video.current;
    if (!element) return;
    const observer = typeof IntersectionObserver === "undefined" ? undefined : new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) { if (!manuallyPaused.current) void play(); }
      else pause();
    });
    if (observer) observer.observe(element);
    else void play();
    return () => { observer?.disconnect(); cancelPlayback(); media?.pause(); };
  }, [pause, play, cancelPlayback]);
  async function toggle() {
    setError(false);
    if (playing) { manuallyPaused.current = true; pause(); return; }
    manuallyPaused.current = false;
    await play(true);
  }
  const validRatio = width !== undefined && height !== undefined && Number.isFinite(width) && Number.isFinite(height) && width > 0 && height > 0;
  const frameStyle = { aspectRatio: validRatio ? `${width} / ${height}` : "1 / 1", maxHeight: "60dvh" };
  const mediaClassName = "absolute inset-0 block h-full w-full object-contain";
  const fail = () => { cancelPlayback(); setError(true); setPlaying(false); };
  const media = mp4 || webm || mp4Blob || webmBlob ? <video ref={video} autoPlay loop muted playsInline preload="metadata" poster={poster} aria-label={alt || "Animated GIF"} width={validRatio ? width : undefined} height={validRatio ? height : undefined} onError={fail} className={mediaClassName} onPause={() => setPlaying(false)} onPlay={() => setPlaying(true)}>
      {webm || webmBlob ? <source ref={webmSource} src={webm} type="video/webm" /> : null}{mp4 || mp4Blob ? <source ref={mp4Source} src={mp4} type="video/mp4" /> : null}
    </video> : playing ? <img ref={image} src={source || undefined} alt={alt} width={validRatio ? width : undefined} height={validRatio ? height : undefined} className={mediaClassName} onError={fail} /> : poster || posterBlob ? <img ref={image} src={poster} alt={alt} width={validRatio ? width : undefined} height={validRatio ? height : undefined} className={mediaClassName} onError={fail} /> : <span className="absolute inset-0 flex items-center justify-center p-4 text-sm">Animated GIF · {alt}</span>;
  return <div ref={container} className="flex min-w-0 flex-col gap-2">
    <div data-gif-viewport className="relative block w-full overflow-hidden rounded-xl bg-muted" style={frameStyle}>
      {onOpenViewer ? <button type="button" aria-label="Open GIF Viewer" className="absolute inset-0 block h-full w-full outline-offset-4 focus-visible:outline-2 focus-visible:outline-ring" onClick={event => { event.stopPropagation(); onOpenViewer(); }}>{media}</button> : media}
      {error ? <p role="alert" className="pointer-events-none absolute inset-x-0 bottom-0 bg-background/90 p-2 text-sm text-destructive">GIF Could Not Be Played. Try Again.</p> : null}
    </div>
    <div className="flex items-center gap-2"><span className="text-xs font-medium">GIF</span><Button type="button" variant="outline" size="sm" onClick={event => { event.stopPropagation(); void toggle(); }} aria-label={playing ? "Pause GIF" : "Play GIF"}>{playing ? "Pause" : "Play"}</Button></div>
  </div>;
}
