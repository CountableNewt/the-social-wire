import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Navigation from "next/navigation";
import * as Auth from "@/hooks/useAuth";
import * as Finance from "@/lib/financeFeedClient";
import * as Sports from "@/lib/sportsFeedClient";
import * as Circle from "@/lib/circleFeedClient";
import * as Gateway from "@/lib/socialWireGatewayClient";
import { AppSidebar } from "@/components/AppSidebar/AppSidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { PublicationSidebarProvider } from "@/contexts/PublicationSidebarContext";
import { ReadRouteProvider } from "@/contexts/ReadRouteContext";
import { rememberBrowserSportsVisibility } from "@/lib/sportsTopicVisibility";
import ReadIndexPage from "@/app/read/page";
import * as ReadPubPage from "@/app/read/[...pubId]/ReadPubPage";
import { recordTabUsage } from "@/lib/tabUsageTelemetry";
import { recordClientPerformance } from "@/lib/clientPerformanceTelemetry";
import type { OAuthSession } from "@atproto/oauth-client-browser";
const restores: (() => void)[] = [];
let client: QueryClient;
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const auth = spyOn(Auth, "useAuth").mockReturnValue({ session: null, getOAuthSession: () => null, isLoading: false, oauthSessionReloadSeq: 0, signOut: async () => {} } as ReturnType<typeof Auth.useAuth>);
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ push() {}, replace() {} } as unknown as ReturnType<typeof Navigation.useRouter>);
  const path = spyOn(Navigation, "usePathname").mockReturnValue("/read");
  const params = spyOn(Navigation, "useSearchParams").mockReturnValue(new URLSearchParams("feed=subscribed") as ReturnType<typeof Navigation.useSearchParams>);
  restores.push(...[auth, router, path, params].map(spy => () => spy.mockRestore()));
  const media = Object.getOwnPropertyDescriptor(window, "matchMedia");
  Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) });
  restores.push(() => { if (media) Object.defineProperty(window, "matchMedia", media); else Reflect.deleteProperty(window, "matchMedia"); });
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });

it("does not request retired catalogs at reader startup or restore their navigation from enabled caches", async () => {
  const finance = spyOn(Finance, "getFinanceCatalog").mockResolvedValue({ enabled: true, available: true, widgetsEnabled: true, feeds: [] });
  const sports = spyOn(Sports, "getSportsCatalog").mockResolvedValue({ enabled: true, available: true, eventsEnabled: true, feeds: [], entities: [], version: "retired-cache" });
  const circle = spyOn(Circle, "getCircleCatalog").mockResolvedValue({ enabled: true, available: true, title: "Your Circle", subtitle: "", supportedLanguages: [] });
  restores.push(...[finance, sports, circle].map(spy => () => spy.mockRestore()));
  client.setQueryData(["financeCatalog"], { enabled: true, available: true, feeds: [] });
  client.setQueryData(["sportsCatalog"], { enabled: true, available: true, feeds: [] });
  rememberBrowserSportsVisibility("local", true, Date.now());
  const observers: PerformanceObserverCallback[] = [];
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "PerformanceObserver");
  Object.defineProperty(globalThis, "PerformanceObserver", { configurable: true, value: class { constructor(callback: PerformanceObserverCallback) { observers.push(callback); } observe() {} disconnect() {} } });
  restores.push(() => { if (descriptor) Object.defineProperty(globalThis, "PerformanceObserver", descriptor); else Reflect.deleteProperty(globalThis, "PerformanceObserver"); });
  await act(async () => { render(<QueryClientProvider client={client}><SidebarProvider><PublicationSidebarProvider><ReadRouteProvider><AppSidebar selectedPubId={null} onSelectPub={() => {}} /></ReadRouteProvider></PublicationSidebarProvider></SidebarProvider></QueryClientProvider>); });
  await act(async () => { for (const callback of observers) callback({ getEntries: () => [{ name: "socialwire.feed.first-content" }] } as unknown as PerformanceObserverEntryList, {} as PerformanceObserver); });
  expect(finance).not.toHaveBeenCalled(); expect(sports).not.toHaveBeenCalled(); expect(circle).not.toHaveBeenCalled();
  for (const label of ["Sports", "Finance", "Your Circle", "The Wire"]) expect(screen.queryByText(label, { exact: true })).toBeNull();
  expect(screen.getAllByText("Subscribed", { exact: true }).length).toBeGreaterThan(0);
});

it("does not send unsupported tab usage while preserving accepted feed performance telemetry", async () => {
  const gateway = spyOn(Gateway, "gatewayFetch").mockResolvedValue(new Response(null, { status: 202 })); restores.push(() => gateway.mockRestore());
  const session = { did: "did:plc:viewer" } as unknown as OAuthSession;
  for (const event of ["tab_open", "tab_dwell"] as const) expect((await recordTabUsage(session, { event, tab: "subscribed", durationMs: 50 })).status).toBe(204);
  expect(gateway).not.toHaveBeenCalled();
  expect((await recordClientPerformance(session, { event: "cached_feed_paint", durationMs: 50, feedType: "aggregate", cacheState: "hit", outcome: "success" })).status).toBe(202);
  expect(gateway).toHaveBeenCalledTimes(1);
});


it.each(["wire", "circle", "finance", "sports"])("redirects a stale %s URL without mounting a retired reader", async feed => {
  const params = spyOn(Navigation, "useSearchParams").mockReturnValue(new URLSearchParams(`feed=${feed}`) as ReturnType<typeof Navigation.useSearchParams>);
  const redirects: string[] = [];
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace: (path: string) => redirects.push(path) } as unknown as ReturnType<typeof Navigation.useRouter>);
  const mounted: unknown[] = [];
  const reader = spyOn(ReadPubPage, "default").mockImplementation(({ aggregateFeed }) => { mounted.push(aggregateFeed); return <div>Subscribed Reader</div>; });
  restores.push(...[params, router, reader].map(spy => () => spy.mockRestore()));
  await act(async () => { render(<ReadIndexPage />); });
  await waitFor(() => expect(redirects).toContain("/read?feed=subscribed"));
  expect(mounted.length).toBeGreaterThan(0);
  expect(mounted.every(feed => JSON.stringify(feed) === JSON.stringify({ kind: "subscribed" }))).toBe(true);
});
