import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { DEFAULT_FEED_DISPLAY_PREFERENCES } from "@/lib/feedPreferences";

const browserGlobals = {
  requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0),
  cancelAnimationFrame: (handle: ReturnType<typeof setTimeout>) => clearTimeout(handle),
  HTMLElement: window.HTMLElement,
  Element: window.Element,
  Node: window.Node,
  getComputedStyle: window.getComputedStyle.bind(window),
};
const originalGlobals = new Map(Object.keys(browserGlobals).map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
beforeAll(() => {
  for (const [key, value] of Object.entries(browserGlobals)) Object.defineProperty(globalThis, key, { configurable: true, writable: true, value });
});
afterAll(() => {
  for (const [key, descriptor] of originalGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
});

const restorers: Array<() => void> = [];
async function replace(path: string, name: string, implementation: unknown) {
  const exports = await import(path);
  const spy = spyOn(exports, name).mockImplementation(implementation as never);
  restorers.push(() => spy.mockRestore());
}
const push = mock(() => undefined);
const selectTab = mock(() => undefined);
const selectPub = mock(() => undefined);
let pathname = "/read";
let params = new URLSearchParams();
let wide = false;
let semble = false;
const subscribed = { publicationId: "subscribed-publication", title: "Subscribed Publication" };
const following = { publicationId: "following-publication", title: "Following Publication" };
const noop = () => undefined;
const wrapper = ({ children }: { children?: ReactNode }) => <div>{children}</div>;

beforeEach(async () => {
  pathname = "/read"; params = new URLSearchParams(); wide = false; semble = false;
  push.mockClear(); selectTab.mockClear(); selectPub.mockClear();
  const hooks: Array<[string, string, unknown]> = [
    ["@/lib/algorithmicFeeds", "algorithmicFeedsEnabled", () => true],
    ["next/navigation", "useRouter", () => ({ push, replace: noop })],
    ["next/navigation", "usePathname", () => pathname],
    ["next/navigation", "useSearchParams", () => params],
    ["@/hooks/useAuth", "useAuth", () => ({ session: { did: "did:plc:viewer" }, signOut: noop })],
    ["@/hooks/useSocialUnreadCounts", "useSocialUnreadCounts", () => ({})],
    ["@/hooks/useClientHydrated", "useClientHydrated", () => true],
    ["@/hooks/useDesktopPublicationRail", "useDesktopPublicationRail", () => wide],
    ["@/hooks/useStandardReaderLists", "useStandardReaderLists", () => ({ lists: [{ uri: "list-one", name: "List One" }], query: {}, signedIn: false })],
    ["@/hooks/useStandardReaderLists", "useStandardReaderList", () => ({ data: { name: "List One" }, refetch: noop })],
    ["@/contexts/SidebarChromeContext", "useSidebarChrome", () => ({ publicationTab: "following", setPublicationTab: selectTab, sidebarExpandedKeys: new Set(), setSelectedFolderUri: noop, toggleSidebarExpandedKey: noop, syncSidebarFolderExpandKeys: noop })],
    ["@/contexts/ReadStateContext", "useReadState", () => ({ isEntryRead: () => false })],
    ["@/contexts/ReadSidebarScopeContext", "useReadSidebarScopeOptional", () => null],
    ["@/contexts/PublicationSidebarContext", "useSidebarProjection", () => ({ folders: [], prefsMap: new Map(), allPublicationRows: [subscribed, following], folderMap: new Map(), unfolderedPubs: [subscribed], followingTabPublications: [following], unreadCountsByPublicationId: new Map() })],
    ["@/contexts/PublicationSidebarContext", "useSidebarBootstrap", () => ({ hasSidebarSnapshot: true, bootstrapStreamComplete: true })],
    ["@/hooks/useReadLaterPreferences", "useConfiguredReadLaterService", () => ({ serviceId: semble ? "semble" : "latr", sembleConnection: { collectionName: "My Collection" } })],
    ["@/hooks/useLatrSaved", "useLatrMergedHttpsSaves", (status: string) => ({ data: [{ subjectUri: status, source: status === "active" ? "Active Source" : "Archived Source" }] })],
    ["@/hooks/useSembleReadLater", "useSembleCollectionItems", () => ({ items: [] })],
    ["@/hooks/useFeedDisplayPreferences", "useFeedDisplayPreferences", () => ({ preferences: { ...DEFAULT_FEED_DISPLAY_PREFERENCES, visibleFeeds: ["readLater", "archive", "subscribed", "following"], showWire: true, showCircle: true, showFinance: true, showSports: true, showSocial: false, showPodcasts: false } })],
    ["@/hooks/useDeferredFeedCatalogs", "useDeferredFeedCatalogs", () => true],
    ["@/hooks/useWireFeed", "useWireFeedCatalog", () => ({ data: { enabled: true, available: true } })],
    ["@/hooks/useSportsCatalog", "useSportsCatalog", () => ({ confirmedEnabled: true })],
    ["@/hooks/useFinanceCatalog", "useFinanceCatalog", () => ({ data: { enabled: true } })],
    ["@/hooks/useViewerProfile", "useViewerProfile", () => ({})],
    ["@/hooks/useSidebarUnreadController", "useSidebarUnreadController", () => new Map()],
    ["@/hooks/useCrossClientReadSync", "useCrossClientReadSync", noop],
    ["@/hooks/usePrefetchSidebarPublicationEntries", "usePrefetchSidebarPublicationEntries", noop],
    ["@/lib/savedFeedSources", "savedFeedSources", (rows: Array<{ source: string }>) => rows.map(row => ({ key: row.source, name: row.source, count: 1 }))],
    ["@/components/AppSidebar/SidebarReadBulkMenuWrap", "SidebarReadBulkMenuWrap", wrapper],
    ["@/components/AppSidebar/SidebarFoldersSection", "SidebarFoldersSection", () => <li>Folder Sources</li>],
    ["@/components/AppSidebar/SidebarPublicationsSection", "SidebarPublicationsSection", ({ publications, onSelectPub }: { publications: Array<typeof subscribed>; onSelectPub: (id: string) => void }) => <li>{publications.map(publication => <button key={publication.publicationId} onClick={() => onSelectPub(publication.publicationId)}>{publication.title}</button>)}</li>],
    ["@/components/AppSidebar/SidebarListPublicationsSection", "SidebarListPublicationsSection", () => <li>List Publication</li>],
    ["@/components/AppSidebar/SidebarTopicsSection", "SidebarTopicsSection", ({ compact }: { compact: boolean }) => <div>Topics{compact ? <><span>Symbol One</span><span>Team One</span></> : null}</div>],
    ["@/components/AppSidebar/SidebarListsSection", "SidebarListsSection", ({ selectedPublications }: { selectedPublications?: ReactNode }) => <div>Lists<span>List One</span>{selectedPublications}</div>],
    ["@/components/shared/FloatingChromeSurface", "FloatingChromeSurface", wrapper],
    ["@/components/Social/SocialProfileLink", "SocialProfileLink", wrapper],
  ];
  for (const [path, name, implementation] of hooks) await replace(path, name, implementation);
  for (const name of ["AppSidebarBrandHeader", "SidebarToolsSection", "FeedbackDialog"]) await replace(`@/components/AppSidebar/${name}`, name, () => null);
  for (const name of ["Sidebar", "SidebarContent", "SidebarFooter", "SidebarGroup", "SidebarGroupLabel", "SidebarMenu", "SidebarMenuItem", "SidebarMenuSub", "SidebarMenuBadge"]) {
    await replace("@/components/ui/sidebar", name, ({ children, "aria-label": label }: { children?: ReactNode; "aria-label"?: string }) => <div aria-label={label}>{children}</div>);
  }
  await replace("@/components/ui/sidebar", "SidebarMenuButton", ({ children, onClick, role, "aria-label": label }: { children?: ReactNode; onClick?: () => void; role?: string; "aria-label"?: string }) => <button role={role} aria-label={label} onClick={onClick}>{children}</button>);
  await replace("@/components/ui/sidebar", "SidebarResizeHandle", () => null);
  await replace("@/components/ui/sidebar", "useSidebar", () => ({ setOpenMobile: noop }));
});
afterEach(() => { cleanup(); for (const restore of restorers.splice(0).reverse()) restore(); });
async function mount() {
  const { AppSidebar } = await import("@/components/AppSidebar/AppSidebar");
  await act(async () => { render(<AppSidebar selectedPubId={null} onSelectPub={selectPub} />); });
}

describe("AppSidebar responsive source composition", () => {
  it("hides all suspended algorithmic feeds even with enabled catalogs and saved preferences", async () => {
    await replace("@/lib/algorithmicFeeds", "algorithmicFeedsEnabled", () => false);
    await mount();
    for (const label of ["The Wire", "Your Circle", "Finance", "Sports"]) {
      expect(screen.queryByText(label)).toBeNull();
    }
    expect(screen.getByText("Subscribed Publication")).toBeDefined();
  });
  for (const route of ["/read", "/saved", "/archive", "/social"]) {
    it(`keeps every compact source tree available from ${route}`, async () => {
      pathname = route; await mount();
      for (const name of ["Subscribed Publication", "Following Publication", "Folder Sources", "Symbol One", "Team One", "List One"]) expect(screen.getAllByText(name)).toHaveLength(1);
      expect(within(screen.getByLabelText("Publication Source")).getByRole("button", { name: "The Wire, Beta" })).toBeDefined();
      expect(within(screen.getByLabelText("Publication Source")).getByRole("button", { name: "Your Circle, Beta" })).toBeDefined();
      const active = screen.getByLabelText("Read Later Publications");
      const archived = screen.getByLabelText("Archive Publications");
      expect(within(active).getByText("Active Source")).toBeDefined();
      expect(within(active).queryByText("Archived Source")).toBeNull();
      expect(within(archived).getByText("Archived Source")).toBeDefined();
      fireEvent.click(within(archived).getByRole("button"));
      expect(push).toHaveBeenCalledWith("/archive?source=Archived%20Source");
      fireEvent.click(within(active).getByRole("button"));
      expect(push).toHaveBeenCalledWith("/saved?source=Active%20Source");
    });
  }
  it("sets the source feed before opening subscribed and following publications", async () => {
    pathname = "/saved"; await mount();
    fireEvent.click(screen.getByRole("button", { name: "Subscribed Publication" }));
    expect(selectTab).toHaveBeenLastCalledWith("subscribed");
    expect(selectPub).toHaveBeenLastCalledWith("subscribed-publication");
    fireEvent.click(screen.getByRole("button", { name: "Following Publication" }));
    expect(selectTab).toHaveBeenLastCalledWith("following");
    expect(selectPub).toHaveBeenLastCalledWith("following-publication");
  });
  it("nests selected list publications in compact navigation", async () => {
    params = new URLSearchParams({ list: "list-one" }); await mount();
    expect(screen.getAllByText("List Publication")).toHaveLength(1);
    expect(screen.getByText("List Publication").parentElement?.textContent).toContain("Lists");
  });
  it("omits Archive and saved publication sources for Semble", async () => {
    semble = true; await mount();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
    expect(screen.queryByLabelText("Archive Publications")).toBeNull();
    expect(screen.queryByLabelText("Read Later Publications")).toBeNull();
    expect(screen.getByRole("button", { name: "My Collection" })).toBeDefined();
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "More Feeds" }));
      await new Promise(resolve => setTimeout(resolve, 0));
    });
    expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull();
    expect(screen.getByRole("menuitem", { name: "My Collection" })).toBeDefined();
  });
  it("keeps wide sources in the active rail without duplicating compact trees", async () => {
    wide = true; params = new URLSearchParams({ feed: "subscribed" }); await mount();
    expect(screen.getAllByText("Subscribed Publication")).toHaveLength(1);
    expect(screen.queryByText("Following Publication")).toBeNull();
    expect(screen.queryByLabelText("Read Later Publications")).toBeNull();
    expect(screen.queryByLabelText("Archive Publications")).toBeNull();
    expect(screen.queryByText("Symbol One")).toBeNull();
  });
});
