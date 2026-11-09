import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as Navigation from "next/navigation";
import * as Auth from "@/hooks/useAuth";
import * as Brands from "@/hooks/useBrandAccounts";
import * as Analytics from "@/hooks/useCreatorAnalytics";
import * as Mobile from "@/hooks/use-mobile";
import { SidebarProvider } from "@/components/ui/sidebar";
import AnalyticsPage from "@/app/analytics/page";
import AnalyticsLayout from "@/app/analytics/layout";
import type { BlueskyAnalyticsSummary } from "@/lib/analytics/blueskyAnalyticsClient";

const restores: (() => void)[] = [];
let brands: string[];
let auth: ReturnType<typeof Auth.useAuth>;
let state: ReturnType<typeof Analytics.useCreatorAnalytics>;
const report: BlueskyAnalyticsSummary = {
  sampledAt: "2026-11-09T12:00:00Z", days: 30,
  profile: { followers: 42, following: 5, posts: 10 },
  activity: { posts: 3, replies: 1, postsPerDay: 0.1 },
  topPosts: [{ uri: "at://did:plc:alice/app.bsky.feed.post/abc", title: "My Post", url: "https://bsky.app/post", publishedAt: "2026-11-08T12:00:00Z", engagement: { likes: 3, replies: 2, reposts: 1, quotes: 0 } }],
  shares: [],
  coverage: { feedComplete: true, feedAvailable: true, sharesComplete: false, sharesAvailable: true, articlesSearched: 0, feedPostsExamined: 4 },
};
beforeEach(() => {
  brands = [];
  const brandSpy = spyOn(Brands, "useBrandAccounts").mockImplementation(() => brands); restores.push(() => brandSpy.mockRestore());
  auth = { session: { did: "did:plc:alice" }, isLoading: false } as ReturnType<typeof Auth.useAuth>;
  const authSpy = spyOn(Auth, "useAuth").mockImplementation(() => auth); restores.push(() => authSpy.mockRestore());
  const mobile = spyOn(Mobile, "useIsMobile").mockReturnValue(false); restores.push(() => mobile.mockRestore());
  const media = Object.getOwnPropertyDescriptor(window, "matchMedia");
  Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) });
  restores.push(() => { if (media) Object.defineProperty(window, "matchMedia", media); else Reflect.deleteProperty(window, "matchMedia"); });
  state = { blueskyQuery: { data: report, isPending: false, isFetching: false, error: null, refetch: mock(async () => {}) }, refresh: { error: null, isPending: false }, collect: mock(async () => {}) } as unknown as ReturnType<typeof Analytics.useCreatorAnalytics>;
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });
function hook() { const value = spyOn(Analytics, "useCreatorAnalytics").mockImplementation(() => state); restores.push(() => value.mockRestore()); return value; }
async function mount() { const spy = hook(); await act(async () => { render(<SidebarProvider><AnalyticsPage /></SidebarProvider>); }); return spy; }

test("shows current AppView metrics without unsupported traffic, historical graphs or duplicate reports", async () => {
  await mount();
  expect(screen.getByRole("region", { name: "Bluesky Analytics" })).toBeTruthy();
  expect(screen.getByText("42")).toBeTruthy();
  expect(screen.getByText("Followers")).toBeTruthy();
  expect(screen.getByRole("region", { name: "Top Bluesky Posts" })).toBeTruthy();
  expect(screen.getByRole("region", { name: "Discovered Article Shares" })).toBeTruthy();
  expect(screen.getByRole("link", { name: "My Post" }).getAttribute("href")).toContain("/social/post?");
  for (const label of ["Combined Totals", "Social Wire Activity", "Social Wire Views", "Impressions", "Views", "Outbound Clicks", "Click-Through Rate", "CTR", "Content Performance", "Bluesky Like Snapshots", "Combined Social Wire Views"]) {
    expect(screen.queryByText(label)).toBeNull();
  }
  expect(screen.queryByRole("img")).toBeNull();
  expect(screen.queryByRole("button", { name: "Show More" })).toBeNull();
});

test("refreshes the selected account and changes the AppView time range", async () => {
  const spy = await mount();
  fireEvent.change(screen.getByRole("combobox", { name: "Analytics Time Range" }), { target: { value: "7" } });
  expect(spy.mock.calls.at(-1)).toEqual([7, "did:plc:alice"]);
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(state.collect).toHaveBeenCalledTimes(1));
});

test("displays AppView loading and recoverable errors without inventing a report", async () => {
  state = { ...state, blueskyQuery: { ...state.blueskyQuery, data: undefined, isPending: true } } as unknown as typeof state; hook();
  const view = render(<SidebarProvider><AnalyticsPage /></SidebarProvider>);
  expect(screen.getByRole("status").textContent).toBe("Loading Bluesky Analytics…");
  expect(screen.queryByText("Followers")).toBeNull();
  state = { ...state, blueskyQuery: { ...state.blueskyQuery, isPending: false, error: new Error("Provider Unavailable") } } as unknown as typeof state;
  view.rerender(<SidebarProvider><AnalyticsPage /></SidebarProvider>);
  expect(screen.getByRole("alert").textContent).toContain("Provider Unavailable");
  fireEvent.click(screen.getByRole("button", { name: "Retry Bluesky Analytics" })); expect(state.blueskyQuery.refetch).toHaveBeenCalledTimes(1);
});

test("keeps current AppView data visible if collecting article URLs fails", async () => {
  state = { ...state, refresh: { ...state.refresh, error: new Error("Article Collection Unavailable") } } as unknown as typeof state;
  await mount();
  expect(screen.getByText("42")).toBeTruthy();
  expect(screen.getByRole("alert").textContent).toContain("Article Collection Unavailable");
});

test("the Analytics route waits for authentication and redirects signed-out viewers without private children", async () => {
  const replace = mock<(path: string) => void>(() => {});
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace } as unknown as ReturnType<typeof Navigation.useRouter>); restores.push(() => router.mockRestore());
  auth = { ...auth, session: null, isLoading: true };
  const view = render(<AnalyticsLayout><p>Private Analytics</p></AnalyticsLayout>);
  expect(screen.getByRole("status").textContent).toBe("Loading Analytics…"); expect(replace).not.toHaveBeenCalled();
  auth = { ...auth, isLoading: false }; view.rerender(<AnalyticsLayout><p>Private Analytics</p></AnalyticsLayout>);
  await waitFor(() => expect(replace).toHaveBeenCalledWith("/login"));
  expect(screen.queryByText("Private Analytics")).toBeNull();
  render(<AnalyticsPage />); expect(screen.getByText("Log In to View Analytics.")).toBeTruthy();
});

test("selects brand account reports and removes deselected reports without combined totals", async () => {
  brands = ["did:plc:brand"];
  const spy = await mount();
  expect(screen.queryByRole("region", { name: "Analytics for did:plc:brand" })).toBeNull();
  fireEvent.click(screen.getByRole("checkbox", { name: "did:plc:brand" }));
  expect(screen.getByRole("region", { name: "Analytics for did:plc:brand" })).toBeTruthy();
  expect(spy.mock.calls.some(args => args[1] === "did:plc:brand")).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: "Personal Account" }));
  expect(screen.queryByRole("region", { name: "Analytics for did:plc:alice" })).toBeNull();
  expect(screen.queryByText("Combined Totals")).toBeNull();
  fireEvent.click(screen.getByRole("checkbox", { name: "did:plc:brand" }));
  expect(screen.getByText("Select Accounts to View Analytics.")).toBeTruthy();
});
