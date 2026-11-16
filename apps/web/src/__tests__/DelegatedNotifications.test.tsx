import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Auth from "@/hooks/useAuth";
import * as Accounts from "@/hooks/useBrandAccounts";
import * as Catalog from "@/hooks/useBlueskySocialCatalog";
import * as Discovery from "@/hooks/useSocialDiscovery";
import * as Social from "@/components/BrandAccounts/useBrandAccountSocial";
import * as Mobile from "@/hooks/use-mobile";
import { SocialNotifications } from "@/components/Social/SocialNotifications";
import { SidebarProvider } from "@/components/ui/sidebar";
import type { SocialNotification } from "@/lib/blueskySocialDiscoveryClient";
import { includeBrandNotifications, setIncludeBrandNotifications } from "@/lib/brandAccounts/brandNotificationPreferences";

const owner = "did:plc:owner", brand = "did:plc:brand";
const restores: (() => void)[] = [];
let currentOwner: string; let accounts: string[]; let client: QueryClient;
let brandAction: ReturnType<typeof mock>; let personalAction: ReturnType<typeof mock<(seenAt: string) => void>>;
const moderation = (did: string) => ({ userDid: did, prefs: { adultContentEnabled: false, labels: {}, labelers: [], mutedWords: [], hiddenPosts: [] } });
const follow = (key: string, indexedAt: string): SocialNotification => ({
  uri: `at://did:plc:customer/app.bsky.graph.follow/${key}`,
  cid: "test",
  author: { did: "did:plc:customer", handle: "customer.test" },
  reason: "follow",
  record: { $type: "app.bsky.graph.follow" },
  isRead: false,
  indexedAt,
});
const older = follow("older", "2026-11-12T01:00:00Z");
const newer = follow("newer", "2026-11-12T02:00:00Z");
const latest = follow("latest", "2026-11-12T03:00:00Z");
const secondBrand = "did:plc:second-brand";
const personalPage = mock(async () => {});
const delegatedPage = mock(async () => {});
beforeEach(() => {
  window.localStorage.clear(); currentOwner = owner; accounts = [brand];
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  personalPage.mockClear(); delegatedPage.mockClear();
  brandAction = mock(async () => {}); personalAction = mock<(seenAt: string) => void>(() => {});
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: { did: currentOwner } }) as ReturnType<typeof Auth.useAuth>); restores.push(() => auth.mockRestore());
  const linked = spyOn(Accounts, "useBrandAccounts").mockImplementation(() => accounts); restores.push(() => linked.mockRestore());
  const mobile = spyOn(Mobile, "useIsMobile").mockReturnValue(false); restores.push(() => mobile.mockRestore());
  const catalog = spyOn(Catalog, "useBlueskySocialCatalog").mockImplementation(() => ({ data: { moderation: moderation(currentOwner) } }) as unknown as ReturnType<typeof Catalog.useBlueskySocialCatalog>); restores.push(() => catalog.mockRestore());
  const personal = spyOn(Discovery, "useSocialNotifications").mockReturnValue({ data: { pages: [{ notifications: [newer], posts: [] }] }, isPending: false, hasNextPage: true, fetchNextPage: personalPage } as unknown as ReturnType<typeof Discovery.useSocialNotifications>); restores.push(() => personal.mockRestore());
  const unread = spyOn(Discovery, "useSocialNotificationUnread").mockReturnValue({ data: 2 } as ReturnType<typeof Discovery.useSocialNotificationUnread>); restores.push(() => unread.mockRestore());
  const seen = spyOn(Discovery, "useMarkSocialNotificationsSeen").mockReturnValue({ mutate: personalAction } as unknown as ReturnType<typeof Discovery.useMarkSocialNotificationsSeen>); restores.push(() => seen.mockRestore());
  const social = spyOn(Social, "useBrandAccountSocial").mockImplementation((_owner, did) => ({ connection: { session: { did } as OAuthSession }, moderation: moderation(did), profile: { data: { did, handle: did === secondBrand ? "second.test" : "brand.test" } }, catalog: {}, notifications: { data: { pages: [{ posts: [], notifications: [did === secondBrand ? latest : older] }] }, isPending: false, hasNextPage: did === brand, fetchNextPage: delegatedPage }, unread: { data: 3 }, posts: {}, thread: {}, mutation: { mutate: brandAction }, interactions: {}, refresh: () => {} }) as unknown as ReturnType<typeof Social.useBrandAccountSocial>); restores.push(() => social.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
function mount() { return render(<QueryClientProvider client={client}><SidebarProvider><SocialNotifications /></SidebarProvider></QueryClientProvider>); }

test("merges account-scoped groups newest first and labels every recipient", () => {
  accounts = [brand, secondBrand];
  mount();

  const cards = screen.getAllByRole("region", { name: "Notification: follow" });
  expect(cards.map(card => within(card).getByText(/^For /).textContent)).toEqual([
    "For @second.test", "For Personal Account", "For @brand.test",
  ]);
  expect(screen.getByText("8 Unread")).toBeTruthy();
  expect(Social.useBrandAccountSocial).toHaveBeenCalledWith(owner, brand, undefined, { includePosts: false });
});

test("marks the included accounts as read and pages sources with more results", () => {
  accounts = [brand, secondBrand];
  mount();
  fireEvent.click(screen.getByRole("button", { name: "Mark All As Read" }));
  expect(personalAction).toHaveBeenCalledTimes(1);
  expect(brandAction).toHaveBeenCalledTimes(2);
  expect(brandAction).toHaveBeenCalledWith({ kind: "seen", seenAt: personalAction.mock.calls[0][0] });
  fireEvent.click(screen.getByRole("button", { name: "Load More" }));
  expect(personalPage).toHaveBeenCalledTimes(1);
  expect(delegatedPage).toHaveBeenCalledTimes(1);
});

test("toggle unmounts brand actors and survives remount only for the personal owner", async () => {
  const view = mount();
  fireEvent.click(screen.getByRole("checkbox", { name: "Include Delegated Accounts" }));
  expect(screen.queryByText("For @brand.test")).toBeNull();
  await waitFor(() => expect(screen.getByText("2 Unread")).toBeTruthy());
  expect(includeBrandNotifications(owner)).toBe(false);
  view.unmount(); mount();
  expect((screen.getByRole("checkbox", { name: "Include Delegated Accounts" }) as HTMLInputElement).checked).toBe(false);
  expect(includeBrandNotifications("did:plc:other-owner")).toBe(true);
});

test("unlink and personal account switches remove the old delegated rows immediately", () => {
  const view = mount();
  accounts = []; view.rerender(<QueryClientProvider client={client}><SidebarProvider><SocialNotifications /></SidebarProvider></QueryClientProvider>);
  expect(screen.queryByText("For @brand.test")).toBeNull();
  expect(screen.queryByRole("checkbox", { name: "Include Delegated Accounts" })).toBeNull();
  accounts = [brand]; setIncludeBrandNotifications(owner, false); currentOwner = "did:plc:other-owner";
  view.rerender(<QueryClientProvider client={client}><SidebarProvider><SocialNotifications /></SidebarProvider></QueryClientProvider>);
  expect((screen.getByRole("checkbox", { name: "Include Delegated Accounts" }) as HTMLInputElement).checked).toBe(true);
  expect(Social.useBrandAccountSocial).toHaveBeenLastCalledWith(currentOwner, brand, undefined, { includePosts: false });
});
test("personal unread errors do not masquerade as a fully known combined total", async () => {
  spyOn(Discovery, "useSocialNotificationUnread").mockReturnValue({ data: 2, isError: true, error: new Error("Personal Count Unavailable") } as ReturnType<typeof Discovery.useSocialNotificationUnread>);
  mount();
  expect(screen.getByText("Unread Counts Unavailable")).toBeTruthy();
  expect(screen.getByText("For @brand.test")).toBeTruthy();
  expect(screen.queryByText("5 Unread")).toBeNull();
});


let previousBrandAccountsFlag: string | undefined;
beforeEach(() => { previousBrandAccountsFlag = process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true"; });
afterEach(() => { if (previousBrandAccountsFlag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = previousBrandAccountsFlag; });

test("does not mount brand notifications or add their unread counts when disabled", () => {
  setIncludeBrandNotifications(owner, true);
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  mount();
  expect(screen.queryByRole("checkbox", { name: "Include Delegated Accounts" })).toBeNull();
  expect(screen.queryByText("For @brand.test")).toBeNull();
  expect(screen.getByText("2 Unread")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Mark All As Read" })).toBeTruthy();
  expect(Social.useBrandAccountSocial).not.toHaveBeenCalled();
});
