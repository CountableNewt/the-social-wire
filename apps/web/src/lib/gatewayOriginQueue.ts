/**
 * Manually-signed requests read the cached DPoP nonce, mint a proof, and only refresh the
 * cache once the response comes back. Concurrent callers (e.g. bulk mark-read/unread, which
 * fire one write per entry) all read the same stale nonce before any of them return, so the
 * gateway accepts one and rejects the rest with 400. Serializing dispatch per gateway origin
 * makes each request observe the previous one's refreshed nonce instead of racing it.
 *
 * One slow request must not stall every later Gateway call (draft sync, read state), so a
 * running task holds the origin for at most `holdMs`; later tasks then proceed and recover
 * any nonce race through the existing DPoP-Nonce retry. Tasks whose caller already aborted
 * are skipped when their turn comes.
 */
export const GATEWAY_ORIGIN_QUEUE_HOLD_MS = 5_000;

const gatewayOriginQueues = new Map<string, Promise<void>>();

export function enqueueForGatewayOrigin<T>(
  origin: string,
  task: () => Promise<T>,
  { signal, holdMs = GATEWAY_ORIGIN_QUEUE_HOLD_MS }: { signal?: AbortSignal | null; holdMs?: number } = {}
): Promise<T> {
  const prior = gatewayOriginQueues.get(origin) ?? Promise.resolve();
  let release!: () => void;
  const slot = new Promise<void>((resolve) => { release = resolve; });
  gatewayOriginQueues.set(origin, slot);
  slot.then(() => {
    if (gatewayOriginQueues.get(origin) === slot) gatewayOriginQueues.delete(origin);
  });
  return prior.then(async () => {
    if (signal?.aborted) {
      release();
      throw signal.reason ?? new DOMException("Aborted", "AbortError");
    }
    const hold = setTimeout(release, holdMs);
    try {
      return await task();
    } finally {
      clearTimeout(hold);
      release();
    }
  });
}
