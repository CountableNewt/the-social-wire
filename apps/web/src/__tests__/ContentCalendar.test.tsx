import * as Mobile from "@/hooks/use-mobile";
import { SidebarProvider } from "@/components/ui/sidebar";
import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Scheduling from "@/hooks/useSkejScheduling";
import * as Navigation from "next/navigation";
import ContentCalendarLayout from "@/app/content-calendar/layout";
import * as Auth from "@/hooks/useAuth";
import * as Accounts from "@/hooks/useBrandAccounts";
import * as Profiles from "@/lib/brandAccounts/brandAccountPublicProfile";
import ContentCalendarPage from "@/app/content-calendar/page";
import type { SkejArticleScheduleSummary, SkejScheduleSummary } from "@/lib/skej/skejSchedulingClient";
import { localCalendarDate, localDateTimeValue } from "@/components/ContentCalendar/calendarDates";

let cache: QueryClient;
let state: ReturnType<typeof Scheduling.useSkejScheduling>;
let delegated: typeof state;
const restores: (() => void)[] = [];
const owner = "did:plc:owner", brand = "did:plc:brand";
const time = new Date(Math.ceil((Date.now() + 3_600_000) / 900_000) * 900_000);
const delegatedTime = new Date(time.getTime() + 60_000);
const item = { did: owner, rkey: "post", scheduleUri: `at://${owner}/app.skej.schedule/post`, scheduledAt: time.toISOString(), status: "scheduled", record: { posts: [{ text: "Planned Content" }] }, attempts: 0 } as unknown as SkejScheduleSummary;
const delegatedItem = {
  ...item,
  did: brand,
  scheduleUri: `at://${brand}/app.skej.schedule/post`,
  scheduledAt: delegatedTime.toISOString(),
  record: { posts: [{ text: "Delegated Content" }] },
} as SkejScheduleSummary;
beforeEach(() => {
  const mobile = spyOn(Mobile, "useIsMobile").mockReturnValue(false); restores.push(() => mobile.mockRestore());
  cache = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  state = { schedules: { data: [item], isPending: false, isError: false, isFetching: false, refetch: mock(async () => ({})) }, readiness: { data: { accountDid: owner, connected: true }, isPending: false, isError: false, refetch: mock(async () => ({})) }, pending: false, schedule: mock(async () => item), cancel: mock(async () => {}), reschedule: mock(async () => {}), connectUrl: "https://skej.test/connect" } as unknown as typeof state;
  delegated = { ...state, schedules: { ...state.schedules, data: [delegatedItem, item], refetch: mock(async () => ({})) },
    readiness: { ...state.readiness, data: { accountDid: brand, connected: true } },
    cancel: mock(async () => {}), reschedule: mock(async () => {}) } as unknown as typeof state;
  const hook = spyOn(Scheduling, "useSkejScheduling").mockImplementation((_owner, actor) => actor === brand ? delegated : state); restores.push(() => hook.mockRestore());
  const auth = spyOn(Auth, "useAuth").mockReturnValue({ session: { did: owner } } as ReturnType<typeof Auth.useAuth>); restores.push(() => auth.mockRestore());
  const linked = spyOn(Accounts, "useBrandAccounts").mockReturnValue([]); restores.push(() => linked.mockRestore());
  const profiles = spyOn(Profiles, "getBrandAccountPublicProfile").mockImplementation(async did => ({ did, handle: did === owner ? "owner.test" : "brand.test" })); restores.push(() => profiles.mockRestore());
  for (const [name, value] of Object.entries({ getComputedStyle: window.getComputedStyle.bind(window), requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0), cancelAnimationFrame: clearTimeout })) { const original = Object.getOwnPropertyDescriptor(globalThis, name); Object.defineProperty(globalThis, name, { configurable: true, value }); restores.push(() => { if (original) Object.defineProperty(globalThis, name, original); else Reflect.deleteProperty(globalThis, name); }); }
});
afterEach(async () => { await act(async () => { cleanup(); await new Promise(resolve => setTimeout(resolve, 0)); }); cache.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
function mount() { return render(<QueryClientProvider client={cache}><SidebarProvider><ContentCalendarPage /></SidebarProvider></QueryClientProvider>); }
function setSchedules(account: typeof state, items: SkejScheduleSummary[]) {
  return { ...account, schedules: { ...account.schedules, data: items } as typeof account.schedules };
}
test("rejects the direct Content Calendar route when scheduling is disabled", () => {
  delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
  expect(() => ContentCalendarPage()).toThrow("NEXT_HTTP_ERROR_FALLBACK;404");
  expect(Scheduling.useSkejScheduling).not.toHaveBeenCalled();
});
test("renders selected day posts and confirms cancellation before calling the selected account", async () => {
  mount();
  if (localCalendarDate(time) !== localCalendarDate(new Date())) fireEvent.click(screen.getByRole("button", { name: new RegExp(time.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" })) }));
  expect(screen.getByText("Planned Content")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Cancel Scheduled Post" }));
  expect(state.cancel).not.toHaveBeenCalled();
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel Scheduled Post" }));
  await waitFor(() => expect(state.cancel).toHaveBeenCalledWith(item));
});
test("reschedules in local time and refreshes the server-backed list", async () => {
  mount();
  if (localCalendarDate(time) !== localCalendarDate(new Date())) fireEvent.click(screen.getByRole("button", { name: new RegExp(time.toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" })) }));
  fireEvent.click(screen.getByRole("button", { name: "Reschedule" }));
  const future = new Date(Math.ceil((Date.now() + 86_400_000) / 900_000) * 900_000);
  fireEvent.change(screen.getByLabelText("Publish At"), { target: { value: localDateTimeValue(future) } });
  fireEvent.click(screen.getByRole("button", { name: "Save Time" }));
  await waitFor(() => expect(state.reschedule).toHaveBeenCalledWith(item, new Date(localDateTimeValue(future)).toISOString()));
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(state.schedules.refetch).toHaveBeenCalled();
});
test("shows personal and delegated schedules together and uses the owning account for actions", async () => {
  spyOn(Accounts, "useBrandAccounts").mockReturnValue([brand]);
  const selectedDay = new Date();
  selectedDay.setHours(12, 0, 0, 0);
  const personal = { ...item, scheduledAt: selectedDay.toISOString() };
  const earlier = { ...delegatedItem, scheduledAt: new Date(selectedDay.getTime() - 60_000).toISOString() };
  state = setSchedules(state, [personal]);
  delegated = setSchedules(delegated, [earlier, personal]);
  mount();

  const cards = screen.getAllByRole("article");
  expect(cards.map(card => card.textContent)).toEqual([
    expect.stringContaining("Delegated Content"), expect.stringContaining("Planned Content"),
  ]);
  await within(cards[0]).findByText("@brand.test");
  await within(cards[1]).findByText("@owner.test");
  expect(screen.getByRole("button", { name: /2 posts/i })).toBeTruthy();
  expect(screen.queryByRole("combobox", { name: "Account" })).toBeNull();

  fireEvent.click(within(cards[0]).getByRole("button", { name: "Cancel Scheduled Post" }));
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel Scheduled Post" }));
  await waitFor(() => expect(delegated.cancel).toHaveBeenCalledWith(earlier));
  expect(state.cancel).not.toHaveBeenCalled();

  fireEvent.click(within(cards[0]).getByRole("button", { name: "Reschedule" }));
  const future = new Date(Math.ceil((Date.now() + 86_400_000) / 900_000) * 900_000);
  fireEvent.change(screen.getByLabelText("Publish At"), { target: { value: localDateTimeValue(future) } });
  fireEvent.click(screen.getByRole("button", { name: "Save Time" }));
  await waitFor(() => expect(delegated.reschedule).toHaveBeenCalledWith(earlier, new Date(localDateTimeValue(future)).toISOString()));
  expect(state.reschedule).not.toHaveBeenCalled();

  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  expect(state.schedules.refetch).toHaveBeenCalled();
  expect(delegated.schedules.refetch).toHaveBeenCalled();
});
test("includes scheduled blog posts from both accounts in the day list and month count", async () => {
  spyOn(Accounts, "useBrandAccounts").mockReturnValue([brand]);
  const socialTime = new Date();
  socialTime.setHours(12, 0, 0, 0);
  function blogPost(did: string, title: string, minuteOffset: number): SkejArticleScheduleSummary {
    const scheduledAt = new Date(socialTime.getTime() + minuteOffset * 60_000).toISOString();
    return {
      ...item,
      did,
      rkey: "blog",
      scheduleUri: `at://${did}/at.skej.schedule/blog`,
      scheduledAt,
      record: {
        ...item.record,
        recordType: "site.standard.document",
        posts: [],
        shadowRecord: {
          $type: "site.standard.document",
          title,
          site: `at://${did}/site.standard.publication/blog`,
          publishedAt: scheduledAt,
        },
      },
    };
  }

  const personalBlog = blogPost(owner, "Personal Blog Post", -1);
  const delegatedBlog = blogPost(brand, "Delegated Blog Post", 1);
  state = setSchedules(state, [{ ...item, scheduledAt: socialTime.toISOString() }, personalBlog]);
  delegated = setSchedules(delegated, [delegatedBlog]);
  mount();

  const cards = screen.getAllByRole("article");
  expect(cards.map(card => card.textContent)).toEqual([
    expect.stringContaining("Blog Post · Personal Blog Post"),
    expect.stringContaining("Planned Content"),
    expect.stringContaining("Blog Post · Delegated Blog Post"),
  ]);
  await within(cards[0]).findByText("@owner.test");
  await within(cards[2]).findByText("@brand.test");
  expect(screen.getByRole("button", { name: /3 posts/i })).toBeTruthy();

  fireEvent.click(within(cards[2]).getByRole("button", { name: "Cancel Scheduled Article" }));
  const dialog = screen.getByRole("dialog", { name: "Cancel Scheduled Article?" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Cancel Scheduled Article" }));
  await waitFor(() => expect(delegated.cancel).toHaveBeenCalledWith(delegatedBlog));
  expect(state.cancel).not.toHaveBeenCalled();
});

test("shows connection and load failure recovery without inventing records", () => {
  state = { ...state, readiness: { ...state.readiness, data: { accountDid: owner, connected: false } }, schedules: { ...state.schedules, data: undefined, isError: true } } as typeof state;
  mount();
  expect(screen.getByRole("link", { name: "Connect Scheduling" }).getAttribute("href")).toBe(state.connectUrl);
  expect(screen.getByRole("alert").textContent).toContain("Could Not Be Loaded");
  expect(screen.queryByText("No Posts Scheduled for This Day")).toBeNull();
});

test("protects the calendar route when signed out", async () => {
  spyOn(Auth, "useAuth").mockReturnValue({ session: null, isLoading: false } as ReturnType<typeof Auth.useAuth>);
  const replace = mock(() => {});
  const router = spyOn(Navigation, "useRouter").mockReturnValue({ replace } as unknown as ReturnType<typeof Navigation.useRouter>); restores.push(() => router.mockRestore());
  render(<ContentCalendarLayout><p>Private Calendar</p></ContentCalendarLayout>);
  await waitFor(() => expect(replace).toHaveBeenCalledWith("/login"));
  expect(screen.queryByText("Private Calendar")).toBeNull();
});

test("removes delegated calendar rows immediately when the account is unlinked", () => {
  spyOn(Accounts, "useBrandAccounts").mockReturnValue([brand]);
  const selectedDay = new Date();
  selectedDay.setHours(12, 0, 0, 0);
  delegated = setSchedules(delegated, [{ ...delegatedItem, scheduledAt: selectedDay.toISOString() }]);
  const view = mount();
  expect(screen.getByText("Delegated Content")).toBeTruthy();
  spyOn(Accounts, "useBrandAccounts").mockReturnValue([]);
  view.rerender(<QueryClientProvider client={cache}><SidebarProvider><ContentCalendarPage /></SidebarProvider></QueryClientProvider>);
  expect(screen.queryByText("Delegated Content")).toBeNull();
});

test("keeps the shared refresh action outside the scrolling calendar", () => {
  mount();
  const header = screen.getByRole("heading", { name: "Content Calendar" }).closest("header")!;
  expect(header.classList.contains("floating-glass")).toBe(true);
  expect(header.contains(screen.getByRole("button", { name: "Refresh" }))).toBe(true);
  expect(header.parentElement!.classList.contains("overflow-hidden")).toBe(true);
  expect(header.nextElementSibling!.classList.contains("overflow-y-auto")).toBe(true);
});

let previousSchedulingFlag: string | undefined;
beforeEach(() => { previousSchedulingFlag = process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true"; });
afterEach(() => { if (previousSchedulingFlag === undefined) delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; else process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = previousSchedulingFlag; });


let previousBrandAccountsFlag: string | undefined;
beforeEach(() => { previousBrandAccountsFlag = process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true"; });
afterEach(() => { if (previousBrandAccountsFlag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = previousBrandAccountsFlag; });

test("loads only personal scheduling when delegated accounts are disabled", () => {
  spyOn(Accounts, "useBrandAccounts").mockReturnValue([brand]);
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  mount();
  expect(Scheduling.useSkejScheduling).toHaveBeenLastCalledWith(owner, owner);
  expect(Scheduling.useSkejScheduling).not.toHaveBeenCalledWith(owner, brand);
  expect(Profiles.getBrandAccountPublicProfile).not.toHaveBeenCalled();
});
