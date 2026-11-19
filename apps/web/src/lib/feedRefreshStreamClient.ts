import type { OAuthSession } from "@atproto/oauth-client-browser";

import { gatewayFetch } from "@/lib/socialWireGatewayClient";
import { socialWireXrpc } from "@/lib/socialWireXrpc";

export type FeedRefreshSelector = {
  kind: "subscribed" | "following" | "folder" | "publication" | "list";
  id?: string;
};

export type FeedRefreshStreamEvent =
  | { kind: "started"; total: number }
  | {
      kind: "publication";
      publicationId: string;
      status: "updated" | "checked" | "unavailable";
      indexed: number;
      completed: number;
      total: number;
    }
  | { kind: "done"; total: number; indexed: number; unavailable: number };

const count = (value: unknown) => (Number.isSafeInteger(value) && (value as number) >= 0 ? (value as number) : undefined);

/** Accept only well-formed events; unknown kinds are ignored for forward compatibility. */
export function parseFeedRefreshStreamEvent(raw: unknown): FeedRefreshStreamEvent | undefined {
  if (!raw || typeof raw !== "object") return;
  const event = raw as { kind?: unknown; started?: unknown; publication?: unknown; done?: unknown };
  if (event.kind === "started") {
    const total = count((event.started as { total?: unknown } | undefined)?.total);
    return total === undefined ? undefined : { kind: "started", total };
  }
  if (event.kind === "publication") {
    const payload = event.publication as Record<string, unknown> | undefined;
    const indexed = count(payload?.indexed), completed = count(payload?.completed), total = count(payload?.total);
    const status = payload?.status;
    if (typeof payload?.publicationId !== "string" || indexed === undefined || completed === undefined ||
        total === undefined || (status !== "updated" && status !== "checked" && status !== "unavailable")) return;
    return { kind: "publication", publicationId: payload.publicationId, status, indexed, completed, total };
  }
  if (event.kind === "done") {
    const payload = event.done as Record<string, unknown> | undefined;
    const total = count(payload?.total), indexed = count(payload?.indexed), unavailable = count(payload?.unavailable);
    if (total === undefined || indexed === undefined || unavailable === undefined) return;
    return { kind: "done", total, indexed, unavailable };
  }
}

/** Run one explicit feed refresh, delivering each NDJSON event as the server streams it. */
export async function consumeFeedRefreshStream(args: {
  oauthSession: OAuthSession;
  selector: FeedRefreshSelector;
  signal?: AbortSignal;
  onEvent: (event: FeedRefreshStreamEvent) => void;
}): Promise<void> {
  const { oauthSession, selector, signal, onEvent } = args;
  const res = await gatewayFetch(oauthSession, socialWireXrpc.refreshFeed, {
    method: "POST",
    headers: { Accept: "application/x-ndjson", "Content-Type": "application/json" },
    body: JSON.stringify(selector),
    signal,
  });
  if (!res.ok) throw new Error(`Feed refresh failed (${res.status})`);
  if (!res.body) throw new Error("Feed refresh returned no body");

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let complete = false;
  const consume = (line: string) => {
    if (!line || complete) return;
    let raw: unknown;
    try { raw = JSON.parse(line); } catch { return; }
    const event = parseFeedRefreshStreamEvent(raw);
    if (!event) return;
    onEvent(event);
    if (event.kind === "done") complete = true;
  };
  // Abort must also wake a reader stalled between NDJSON chunks.
  const cancelReader = () => { void reader.cancel().catch(() => undefined); };
  signal?.addEventListener("abort", cancelReader, { once: true });
  try {
    signal?.throwIfAborted();
    while (!complete) {
      const { done, value } = await reader.read();
      signal?.throwIfAborted();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let newline: number;
      while ((newline = buffer.indexOf("\n")) !== -1) {
        consume(buffer.slice(0, newline).trim());
        buffer = buffer.slice(newline + 1);
      }
    }
    consume(buffer.trim());
  } catch (error) {
    if (signal?.aborted) throw signal.reason;
    throw error;
  } finally {
    signal?.removeEventListener("abort", cancelReader);
    cancelReader();
    reader.releaseLock();
  }
  if (!complete) throw new Error("Feed refresh ended before completing");
}
