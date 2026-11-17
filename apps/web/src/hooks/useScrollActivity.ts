"use client";

import { useCallback, useEffect, useRef, type RefObject } from "react";

/** Quiet period after the last scroll event before a gesture counts as settled. */
export const SCROLL_SETTLE_MS = 800;

/**
 * Reports whether the referenced element scrolled within the last `settleMs`.
 * Listens in the capture phase on the document so it works even when the
 * scroll container mounts after this hook (skeleton → virtual list).
 */
export function useScrollActivity(
  ref: RefObject<HTMLElement | null>,
  settleMs = SCROLL_SETTLE_MS,
): () => boolean {
  const lastScrollAt = useRef(Number.NEGATIVE_INFINITY);
  useEffect(() => {
    const onScroll = (event: Event) => {
      if (ref.current && event.target === ref.current) lastScrollAt.current = performance.now();
    };
    document.addEventListener("scroll", onScroll, { capture: true, passive: true });
    return () => document.removeEventListener("scroll", onScroll, { capture: true });
  }, [ref]);
  return useCallback(
    () => performance.now() - lastScrollAt.current < settleMs,
    [settleMs],
  );
}
