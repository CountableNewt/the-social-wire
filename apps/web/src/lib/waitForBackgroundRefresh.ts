/** How often a deferred background refresh re-checks whether it may apply. */
export const BACKGROUND_REFRESH_RECHECK_MS = 250;

/**
 * Resolve once `allowed()` reports true. Background feed refreshes defer (rather
 * than skip) while the reader is actively scrolling, so new articles still land
 * once the list settles instead of waiting for the next poll.
 */
export function waitForBackgroundRefreshAllowed(
  allowed: (() => boolean) | undefined,
  signal: AbortSignal,
): Promise<void> {
  signal.throwIfAborted();
  if (!allowed || allowed()) return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const timer = setInterval(() => {
      if (!allowed()) return;
      cleanup();
      resolve();
    }, BACKGROUND_REFRESH_RECHECK_MS);
    const abort = () => {
      cleanup();
      reject(signal.reason);
    };
    const cleanup = () => {
      clearInterval(timer);
      signal.removeEventListener("abort", abort);
    };
    signal.addEventListener("abort", abort, { once: true });
  });
}
