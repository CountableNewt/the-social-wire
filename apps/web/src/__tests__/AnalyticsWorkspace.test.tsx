import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import * as Navigation from "next/navigation";
import * as Auth from "@/hooks/useAuth";
import * as Brands from "@/hooks/useBrandAccounts";
import * as Analytics from "@/hooks/useCreatorAnalytics";
import * as Mobile from "@/hooks/use-mobile";
import { SidebarProvider } from "@/components/ui/sidebar";
import AnalyticsPage from "@/app/analytics/page";
import AnalyticsLayout from "@/app/analytics/layout";
import type { BlueskyAnalyticsSummary } from "@/lib/analytics/blueskyAnalyticsClient";
import type { PublicationAnalyticsSummary } from "@/lib/analytics/publicationAnalyticsClient";

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
const publicationReport: PublicationAnalyticsSummary = {
  sampledAt: report.sampledAt,
  inventoryComplete: true,
  publications: [{
    uri: "at://did:plc:alice/site.standard.publication/news",
    name: "My Publication",
    url: "https://example.com/news",
    documents: 12,
    subscribers: 7,
    recommendations: 0,
    recommendationCoverageComplete: true,
  }],
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
  state = {
    blueskyQuery: { data: report, isPending: false, isFetching: false, error: null, refetch: mock(async () => {}) },
    publicationQuery: { data: publicationReport, isPending: false, isFetching: false, error: null, refetch: mock(async () => {}) },
    refresh: { error: null, isPending: false },
    collect: mock(async () => {}),
  } as unknown as ReturnType<typeof Analytics.useCreatorAnalytics>;
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

test("shows each publication's current counts and safe website link", async () => {
  await mount();
  const section = within(screen.getByRole("region", { name: "Publication Analytics" }));
  expect(section.getByRole("link", { name: "My Publication" }).getAttribute("href")).toBe("https://example.com/news");
  expect(section.getByText(/Current Public Counts/)).toBeTruthy();
  expect(section.getAllByRole("term").map(term => term.textContent)).toEqual(["Documents", "Subscribers", "Recommendations"]);
  expect(section.getAllByRole("definition").map(value => value.textContent)).toEqual(["12", "7", "0"]);
});

test("labels incomplete and unavailable publication counts and blocks unsafe URLs", async () => {
  state = {
    ...state,
    publicationQuery: {
      ...state.publicationQuery,
      data: {
        ...publicationReport,
        inventoryComplete: false,
        publications: [{
          ...publicationReport.publications[0],
          url: "javascript:alert(1)",
          subscribers: null,
          recommendations: 3,
          recommendationCoverageComplete: false,
        }],
      },
    },
  } as typeof state;
  await mount();
  const section = within(screen.getByRole("region", { name: "Publication Analytics" }));
  expect(section.getByText(/publication inventory is incomplete/)).toBeTruthy();
  expect(section.getByRole("heading", { name: "My Publication" })).toBeTruthy();
  expect(section.queryByRole("link")).toBeNull();
  expect(section.getAllByRole("definition").map(value => value.textContent)).toEqual(["At least 12", "Unavailable", "At least 3"]);
});

test("loads and retries publication analytics independently of Bluesky", () => {
  state = { ...state, publicationQuery: { ...state.publicationQuery, data: undefined, isPending: true } } as typeof state;
  hook();
  const view = render(<SidebarProvider><AnalyticsPage /></SidebarProvider>);
  const section = within(screen.getByRole("region", { name: "Publication Analytics" }));
  expect(section.getByRole("status").textContent).toBe("Loading Publication Analytics…");
  expect(screen.getByText("Followers")).toBeTruthy();

  state = { ...state, publicationQuery: { ...state.publicationQuery, isPending: false, error: new Error("Publication Provider Unavailable") } } as typeof state;
  view.rerender(<SidebarProvider><AnalyticsPage /></SidebarProvider>);
  expect(section.getByRole("alert").textContent).toContain("Publication Provider Unavailable");
  fireEvent.click(section.getByRole("button", { name: "Retry Publication Analytics" }));
  expect(state.publicationQuery.refetch).toHaveBeenCalledTimes(1);
  expect(state.blueskyQuery.refetch).not.toHaveBeenCalled();

  state = { ...state, publicationQuery: { ...state.publicationQuery, error: null, data: { ...publicationReport, publications: [] } } } as typeof state;
  view.rerender(<SidebarProvider><AnalyticsPage /></SidebarProvider>);
  expect(section.getByText("No standard.site publications found for this account.")).toBeTruthy();
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


let previousBrandAccountsFlag: string | undefined;
beforeEach(() => { previousBrandAccountsFlag = process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true"; });
afterEach(() => { if (previousBrandAccountsFlag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = previousBrandAccountsFlag; });

test("hides brand integrations and keeps personal Analytics when the flag is off", async () => {
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  brands = ["did:plc:brand"];
  const spy = await mount();
  expect(screen.queryByRole("link", { name: "Manage Brand Accounts" })).toBeNull();
  expect(screen.queryByRole("group", { name: "Accounts" })).toBeNull();
  expect(screen.queryByRole("region", { name: "Analytics for did:plc:brand" })).toBeNull();
  expect(screen.getByRole("region", { name: "Analytics for did:plc:alice" })).toBeTruthy();
  expect(spy.mock.calls.every(([, did]) => did === "did:plc:alice")).toBe(true);
});
