import { afterEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as gateway from "@/lib/socialWireGatewayClient";
import { podcastMediaPath, podcastRequest } from "@/lib/podcasts/client";

const restores: (() => void)[] = [];
afterEach(() => restores.splice(0).forEach(restore => restore()));

it("renders structured API errors as messages without object coercion", async () => {
  const fetch = spyOn(gateway, "gatewayFetch").mockResolvedValue(new Response(JSON.stringify({ error: { message: "Analysis Is Unavailable" } }), { status: 503 }));
  restores.push(() => fetch.mockRestore());
  await expect(podcastRequest({} as OAuthSession, "getAnalysis?episodeId=episode")).rejects.toThrow("Analysis Is Unavailable");
  fetch.mockResolvedValue(new Response(JSON.stringify({ error: { code: "Unavailable" } }), { status: 503 }));
  await expect(podcastRequest({} as OAuthSession, "getAnalysis?episodeId=episode")).rejects.toThrow("Podcasts request failed (503)");
});

it("sends podcast methods to their app.thesocialwire.podcast XRPC endpoints", async () => {
  const fetch = spyOn(gateway, "gatewayFetch").mockResolvedValue(new Response(JSON.stringify({ revision: 1 }), { status: 200 }));
  restores.push(() => fetch.mockRestore());
  await podcastRequest({} as OAuthSession, "putState", "POST", { expectedRevision: 0 });
  expect(fetch.mock.calls[0]?.[1]).toBe("/xrpc/app.thesocialwire.podcast.putState");
  expect(fetch.mock.calls[0]?.[2]).toMatchObject({ method: "POST", body: JSON.stringify({ expectedRevision: 0 }) });
  expect(podcastMediaPath("a/b")).toBe("/xrpc/app.thesocialwire.podcast.getMedia?episodeId=a%2Fb");
});
