"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { useCachedImageUrl } from "@/hooks/useCachedImageUrl";
import { invalidateCachedImage, resolveDirectImageUrl } from "@/lib/imageBlobCache";

interface CachedImageProps {
  src?: string | null;
  alt: string;
  width: number;
  height: number;
  className?: string;
  loading?: "eager" | "lazy";
  onError?: () => void;
  onLoad?: () => void;
  "aria-hidden"?: boolean;
  draggable?: boolean;
}

/** Remote image: same-origin IndexedDB blob cache; cross-origin direct `src` (no CORS fetch). */
export function CachedImage({
  src,
  alt,
  width,
  height,
  className = "",
  loading = "lazy",
  onError,
  onLoad,
  "aria-hidden": ariaHidden,
  draggable,
}: CachedImageProps) {
  const source = src?.trim() || null;
  const identity = useMemo(() => ({ source }), [source]);
  const active = useRef<typeof identity | undefined>(identity);
  useLayoutEffect(() => { active.current = identity; return () => { active.current = undefined; }; }, [identity]);
  const image = useRef<HTMLImageElement>(null);
  const [visible, setVisible] = useState<typeof identity>();
  const [retry, setRetry] = useState<{ identity: typeof identity; attempt: number }>();
  const retrying = useRef<typeof identity | undefined>(undefined);
  const reportedFailure = useRef<typeof identity | undefined>(undefined);
  const [decodeFailure, setDecodeFailure] = useState<typeof identity>();
  const attempt = retry?.identity === identity ? retry.attempt : 0;
  const directUrl = source ? resolveDirectImageUrl(source) : undefined;
  const enabled = !!directUrl || loading === "eager" || typeof IntersectionObserver === "undefined" || visible === identity;
  const { objectUrl, failed } = useCachedImageUrl(source, { enabled, reload: attempt });

  useEffect(() => {
    const node = image.current;
    if (!source || enabled || !node) return;
    const observer = new IntersectionObserver(entries => {
      if (active.current === identity && entries.some(entry => entry.isIntersecting)) { setVisible(identity); observer.disconnect(); }
    }, { rootMargin: "200px" });
    observer.observe(node);
    return () => observer.disconnect();
  }, [source, enabled, identity]);

  const reportImageError = useCallback(() => {
    if (!source || active.current !== identity) return;
    if (!directUrl && attempt === 0) {
      if (retrying.current === identity) return;
      retrying.current = identity;
      void invalidateCachedImage(source).catch(() => {}).then(() => {
        if (active.current === identity) setRetry({ identity, attempt: 1 });
      }).finally(() => { if (retrying.current === identity) retrying.current = undefined; });
      return;
    }
    if (reportedFailure.current === identity) return;
    if (!directUrl) void invalidateCachedImage(source).catch(() => {});
    reportedFailure.current = identity;
    onError?.();
  }, [source, identity, directUrl, attempt, onError]);

  useEffect(() => { if (failed) reportImageError(); }, [failed, reportImageError]);

  if (!source) return null;
  const terminal = decodeFailure === identity || failed && attempt > 0;
  const displayUrl = terminal || failed ? undefined : objectUrl;

  return (
    /* eslint-disable-next-line @next/next/no-img-element -- arbitrary publisher / PDS URLs */
    <img
      ref={image}
      src={displayUrl}
      alt={alt}
      aria-hidden={ariaHidden}
      onLoad={() => { if (displayUrl) { reportedFailure.current = undefined; onLoad?.(); } }}
      width={width}
      height={height}
      loading={loading}
      decoding="async"
      draggable={draggable}
      referrerPolicy="no-referrer"
      className={className}
      style={displayUrl ? undefined : { visibility: "hidden" }}
      aria-busy={!displayUrl && !terminal}
      onError={() => { if (displayUrl) { if (attempt > 0 && !directUrl) setDecodeFailure(identity); reportImageError(); } }}
    />
  );
}
