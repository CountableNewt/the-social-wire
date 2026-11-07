"use client";

import { useEffect, useMemo, useState } from "react";

import {
  fetchCachedImageObjectUrl,
  resolveDirectImageUrl,
} from "@/lib/imageBlobCache";

type CachedImageState = {
  key: { cacheKey: string | null; directUrl: string | undefined; enabled: boolean; reload: number } | null;
  objectUrl: string | undefined;
  failed: boolean;
};

/**
 * Resolves a remote image URL for display.
 * Bluesky CDN URLs use the same-origin image proxy + IndexedDB cache. Other
 * cross-origin URLs use direct `<img src>` when browser CORS prevents caching.
 */
export function useCachedImageUrl(src: string | null | undefined, options: { enabled?: boolean; reload?: number } = {}): {
  objectUrl: string | undefined;
  failed: boolean;
} {
  const cacheKey = src?.trim() || null;
  const enabled = options.enabled ?? true;
  const reload = options.reload ?? 0;
  const directUrl = useMemo(
    () => (cacheKey ? resolveDirectImageUrl(cacheKey) : undefined),
    [cacheKey]
  );
  // A restored source needs a new resource identity: cleanup already revoked
  // the previous blob even when its remote URL is the same.
  const requestKey = useMemo(() => ({ cacheKey, directUrl, enabled, reload }), [cacheKey, directUrl, enabled, reload]);
  const [state, setState] = useState<CachedImageState>({
    key: null,
    objectUrl: undefined,
    failed: false,
  });

  useEffect(() => {
    if (!enabled || !cacheKey || directUrl) return;

    let cancelled = false;
    let activeObjectUrl: string | undefined;

    void fetchCachedImageObjectUrl(cacheKey, undefined, { reload: reload > 0 })
      .then((url) => {
        if (cancelled) {
          if (url?.startsWith("blob:")) URL.revokeObjectURL(url);
          return;
        }
        activeObjectUrl = url;
        setState({
          key: requestKey,
          objectUrl: url,
          failed: !url,
        });
      })
      .catch(() => {
        if (!cancelled) {
          setState({
            key: requestKey,
            objectUrl: undefined,
            failed: true,
          });
        }
      });

    return () => {
      cancelled = true;
      if (activeObjectUrl?.startsWith("blob:")) URL.revokeObjectURL(activeObjectUrl);
    };
  }, [cacheKey, directUrl, requestKey, enabled, reload]);

  if (!enabled) return { objectUrl: undefined, failed: false };

  if (directUrl) {
    return { objectUrl: directUrl, failed: false };
  }

  if (!cacheKey || state.key !== requestKey) {
    return { objectUrl: undefined, failed: false };
  }

  return { objectUrl: state.objectUrl, failed: state.failed };
}
