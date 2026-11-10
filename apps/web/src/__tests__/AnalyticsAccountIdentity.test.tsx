import { afterEach, beforeEach, expect, spyOn, test } from "bun:test";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import * as CachedImage from "@/hooks/useCachedImageUrl";
import { AnalyticsAccountIdentity } from "@/components/Analytics/AnalyticsAccountIdentity";

const did = "did:plc:alice";
const profile = { did, handle: "alice.test", displayName: "Alice Example", avatar: "https://images.example/alice.png" };
const restores: (() => void)[] = [];
let respond: (request: Request) => Promise<Response>;
let requests: Request[];
beforeEach(() => {
  requests = [];
  respond = async () => Response.json(profile);
  const transport = Object.assign(async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    const request = input instanceof Request ? input : new Request(input, init);
    requests.push(request);
    return respond(request);
  }, { preconnect: globalThis.fetch.preconnect });
  const fetchSpy = spyOn(globalThis, "fetch").mockImplementation(transport); restores.push(() => fetchSpy.mockRestore());
  const images = spyOn(CachedImage, "useCachedImageUrl").mockImplementation(src => ({ objectUrl: src ?? undefined, failed: false })); restores.push(() => images.mockRestore());
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });

test("shows the account avatar with a display name and separate handle in its header", async () => {
  const view = render(<AnalyticsAccountIdentity did={did} personal presentation="header" />);
  await waitFor(() => expect(screen.getByRole("heading", { name: "Alice Example" })).toBeTruthy());
  expect(screen.getByText("alice.test")).toBeTruthy();
  expect(view.container.querySelector("img")?.getAttribute("src")).toBe(profile.avatar);
  expect(screen.queryByText(/\(@alice\.test\)/)).toBeNull();
  expect(screen.queryByText(/· Personal/)).toBeNull();
  expect(requests).toHaveLength(1);
  expect(new URL(requests[0].url).pathname).toBe("/xrpc/app.bsky.actor.getProfile");
  expect(new URL(requests[0].url).searchParams.get("actor")).toBe(did);
  expect(requests[0].headers.get("Authorization")).toBeNull();
  expect(requests[0].headers.get("DPoP")).toBeNull();
});

test("uses the handle when the display name is blank and avatar initials when no image exists", async () => {
  respond = async () => Response.json({ did, handle: "alice.test", displayName: "   " });
  const view = render(<AnalyticsAccountIdentity did={did} personal presentation="header" />);
  await waitFor(() => expect(screen.getByRole("heading", { name: "alice.test" })).toBeTruthy());
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.textContent).not.toContain("· Personal");
});

test("keeps a usable account identity if public profile metadata is unavailable", async () => {
  respond = async () => { throw new Error("Profile Unavailable"); };
  const view = render(<AnalyticsAccountIdentity did={did} personal presentation="header" />);
  await waitFor(() => expect(requests).toHaveLength(1));
  expect(screen.getByRole("heading", { name: "Personal Account" })).toBeTruthy();
  expect(view.container.textContent).toContain(did);
  expect(view.container.querySelector("img")).toBeNull();
});

test("ignores an old profile response after the displayed account changes", async () => {
  let finish!: (response: Response) => void;
  respond = async request => new URL(request.url).searchParams.get("actor") === did
    ? new Promise(resolve => { finish = resolve; })
    : Response.json({ did: "did:plc:bob", handle: "bob.test", displayName: "Bob Example" });
  const view = render(<AnalyticsAccountIdentity did={did} personal presentation="header" />);
  await waitFor(() => expect(finish).toBeDefined());
  view.rerender(<AnalyticsAccountIdentity did="did:plc:bob" personal presentation="header" />);
  await waitFor(() => expect(screen.getByRole("heading", { name: "Bob Example" })).toBeTruthy());
  await act(async () => { finish(Response.json(profile)); });
  expect(screen.queryByRole("heading", { name: "Alice Example" })).toBeNull();
  expect(screen.getByText("bob.test")).toBeTruthy();
  expect(requests[0].signal.aborted).toBe(true);
});

test("retains the compact label used by dormant account-picker checkboxes", async () => {
  render(<label><input type="checkbox" /><AnalyticsAccountIdentity did={did} personal /></label>);
  await waitFor(() => expect(screen.getByRole("checkbox", { name: "Alice Example (@alice.test) · Personal" })).toBeTruthy());
  expect(screen.queryByRole("heading")).toBeNull();
});
