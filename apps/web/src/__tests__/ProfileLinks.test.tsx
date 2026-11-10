import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Links from "@/lib/profileLinks";
import * as ImageCache from "@/hooks/useCachedImageUrl";
import { ProfileLinks } from "@/components/Account/ProfileLinks";

const firstDid = "did:plc:alice";
const rows = [
  { uri: `at://${firstDid}/app.bsky.actor.link/first`, url: "https://website.example/about", title: "My Website", domain: "website.example", iconUrl: "https://website.example/icon.png" },
  { uri: `at://${firstDid}/app.bsky.actor.link/second`, url: "https://newsletter.example/", domain: "newsletter.example" },
];
let cache: QueryClient;
let load: ReturnType<typeof spyOn<typeof Links, "loadProfileLinks">>;
beforeEach(() => {
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  load = spyOn(Links, "loadProfileLinks").mockResolvedValue(rows);
});
afterEach(() => { cleanup(); cache.clear(); mock.restore(); });
function show(did = firstDid) { return render(<QueryClientProvider client={cache}><ProfileLinks did={did} /></QueryClientProvider>); }

test("renders configured links in profile order with title/domain fallback and safe navigation", async () => {
  show(); const list = await screen.findByRole("list", { name: "Profile Links" });
  const links = within(list).getAllByRole("link");
  expect(links.map(link => link.getAttribute("href"))).toEqual(rows.map(row => row.url));
  expect(links[0]?.textContent).toBe("My Websitewebsite.example");
  expect(links[1]?.textContent).toBe("newsletter.example");
  for (const link of links) {
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  }
  expect(load).toHaveBeenCalledWith(firstDid, expect.any(AbortSignal));
  expect(screen.queryByRole("link", { name: "Open on Bluesky" })).toBeNull();
});

test("renders link icons through the shared image cache without adding spoken decoration", async () => {
  const image = spyOn(ImageCache, "useCachedImageUrl").mockReturnValue({ objectUrl: "blob:cached-profile-icon", failed: false });
  show(); const list = await screen.findByRole("list", { name: "Profile Links" });
  expect(image).toHaveBeenCalledWith(rows[0]!.iconUrl, { enabled: true, reload: 0 });
  const icon = list.querySelector("img");
  expect(icon?.getAttribute("src")).toBe("blob:cached-profile-icon");
  expect(icon?.getAttribute("alt")).toBe("");
  expect(icon?.getAttribute("aria-hidden")).toBe("true");
  expect(within(list).queryByRole("img")).toBeNull();
});

test.each(["loading", "empty", "missing DID"])("shows no invented links while %s", async state => {
  if (state === "loading") load.mockImplementation(() => new Promise(() => {}));
  else load.mockResolvedValue([]);
  const { container } = show(state === "missing DID" ? "" : firstDid);
  if (state === "empty") await waitFor(() => expect(cache.getQueryState(["actorProfileLinks", firstDid])?.status).toBe("success"));
  if (state === "missing DID") expect(load).not.toHaveBeenCalled();
  expect(screen.queryByRole("list", { name: "Profile Links" })).toBeNull();
  expect(screen.queryByRole("link")).toBeNull();
  expect(container.textContent).toBe("");
});

test("reports a load error and retries the same actor", async () => {
  load.mockRejectedValueOnce(new Error("Public PDS Unavailable")); show();
  expect(await screen.findByText("Profile Links Could Not Be Loaded.")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Retry Profile Links" }));
  await screen.findByRole("list", { name: "Profile Links" });
  expect(load).toHaveBeenCalledTimes(2);
  expect(load.mock.calls[1]?.[0]).toBe(firstDid);
  expect(screen.queryByText("Profile Links Could Not Be Loaded.")).toBeNull();
});

test("clears previous actor cards during a DID change and ignores stale responses", async () => {
  let resolveOther!: (value: typeof rows) => void;
  let resolveOld!: (value: typeof rows) => void;
  const oldRequest = new Promise<typeof rows>(resolve => { resolveOld = resolve; });
  const otherRequest = new Promise<typeof rows>(resolve => { resolveOther = resolve; });
  const view = show(); await screen.findByRole("link", { name: /My Website/ });
  load.mockReturnValueOnce(oldRequest);
  await act(async () => { void cache.refetchQueries({ queryKey: ["actorProfileLinks", firstDid] }); });
  load.mockReturnValueOnce(otherRequest);
  view.rerender(<QueryClientProvider client={cache}><ProfileLinks did="did:plc:bob" /></QueryClientProvider>);
  expect(screen.queryByRole("link", { name: /My Website/ })).toBeNull();
  await act(async () => resolveOld(rows));
  expect(screen.queryByRole("link", { name: /My Website/ })).toBeNull();
  await act(async () => resolveOther([{ uri: "at://did:plc:bob/app.bsky.actor.link/site", url: "https://bob.example/", title: "Bob's Website", domain: "bob.example", iconUrl: "https://bob.example/icon.png" }]));
  const link = await screen.findByRole("link", { name: /Bob's Website/ });
  expect(link.getAttribute("href")).toBe("https://bob.example/");
  expect(screen.queryByRole("link", { name: /My Website/ })).toBeNull();
});
