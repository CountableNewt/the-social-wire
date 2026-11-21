import { afterEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as gateway from "@/lib/socialWireGatewayClient";
import { consumeFeedRefreshStream, parseFeedRefreshStreamEvent, type FeedRefreshStreamEvent } from "@/lib/feedRefreshStreamClient";

const restores: (() => void)[] = [];
afterEach(() => { while (restores.length) restores.pop()!(); });

function respondWith(chunks: string[], status = 200) {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  const spy = spyOn(gateway, "gatewayFetch").mockResolvedValue(new Response(body, { status }));
  restores.push(() => spy.mockRestore());
  return spy;
}

it("posts the selector and delivers events split across chunk boundaries", async () => {
  const fetch = respondWith([
    '{"kind":"started","started":{"total":2}}\n{"kind":"public',
    'ation","publication":{"publicationId":"a","status":"updated","indexed":3,"completed":1,"total":2}}\n',
    '{"kind":"unknown"}\n{"kind":"done","done":{"total":2,"indexed":3,"unavailable":0}}\n',
  ]);
  const events: FeedRefreshStreamEvent[] = [];
  await consumeFeedRefreshStream({ oauthSession: {} as OAuthSession, selector: { kind: "folder", id: "rkey" }, onEvent: event => events.push(event) });
  expect(fetch.mock.calls[0]![2]).toMatchObject({ method: "POST", body: '{"kind":"folder","id":"rkey"}' });
  expect(String(fetch.mock.calls[0]![1])).toContain("app.thesocialwire.appview.refreshFeed");
  expect(events.map(event => event.kind)).toEqual(["started", "publication", "done"]);
});

it("fails a stream that ends before done or answers with an error status", async () => {
  respondWith(['{"kind":"started","started":{"total":1}}\n']);
  await expect(consumeFeedRefreshStream({ oauthSession: {} as OAuthSession, selector: { kind: "subscribed" }, onEvent: () => {} }))
    .rejects.toThrow("ended before completing");
  restores.pop()!();
  respondWith([], 503);
  await expect(consumeFeedRefreshStream({ oauthSession: {} as OAuthSession, selector: { kind: "subscribed" }, onEvent: () => {} }))
    .rejects.toThrow("503");
});

it("rejects malformed publication payloads", () => {
  expect(parseFeedRefreshStreamEvent({ kind: "publication", publication: { publicationId: "a", status: "lost", indexed: 0, completed: 1, total: 1 } })).toBeUndefined();
  expect(parseFeedRefreshStreamEvent({ kind: "publication", publication: { publicationId: "a", status: "checked", indexed: -1, completed: 1, total: 1 } })).toBeUndefined();
  expect(parseFeedRefreshStreamEvent({ kind: "started", started: { total: 4 } })).toEqual({ kind: "started", total: 4 });
});
