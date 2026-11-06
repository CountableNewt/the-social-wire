import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  mock,
  spyOn,
  test,
} from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { PublicationTabs } from "@/components/AppSidebar/PublicationTabs";
import { SidebarTopicsSection } from "@/components/AppSidebar/SidebarTopicsSection";
import { SidebarProvider } from "@/components/ui/sidebar";
import * as BulkReadActions from "@/hooks/useCachedBulkReadActions";

beforeEach(() => {
  spyOn(BulkReadActions, "useCachedBulkReadActions").mockReturnValue({
    cachedEntryIds: [],
    bulkDisabled: false,
    isMarkingRead: false,
    error: null,
    applyMarkAllRead: async () => true,
    applyMarkAllUnread: () => undefined,
  });
});

afterEach(() => {
  cleanup();
  mock.restore();
});

describe("PublicationTabs", () => {
  beforeAll(() => {
    Object.defineProperty(globalThis, "Element", {
      configurable: true,
      value: window.Element,
    });
    Object.defineProperty(globalThis, "HTMLElement", {
      configurable: true,
      value: window.HTMLElement,
    });
    Object.defineProperty(globalThis, "Node", {
      configurable: true,
      value: window.Node,
    });
    Object.defineProperty(globalThis, "DOMRect", {
      configurable: true,
      value: window.DOMRect,
    });
    Object.defineProperty(globalThis, "getComputedStyle", {
      configurable: true,
      value: window.getComputedStyle.bind(window),
    });
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: () => ({
        matches: false,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
      }),
    });
    Object.defineProperty(globalThis, "requestAnimationFrame", {
      configurable: true,
      value: (callback: FrameRequestCallback) =>
        setTimeout(() => callback(performance.now()), 0),
    });
    Object.defineProperty(globalThis, "cancelAnimationFrame", {
      configurable: true,
      value: (id: ReturnType<typeof setTimeout>) => clearTimeout(id),
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("shows only active feeds by default and ignores retired feeds in stale visibility sets", () => {
    const { rerender } = render(
      <SidebarProvider>
        <PublicationTabs activeTab={null} onTabChange={() => undefined} />
      </SidebarProvider>,
    );
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    rerender(
      <SidebarProvider>
        <PublicationTabs
          activeTab={null}
          onTabChange={() => undefined}
          visibleFeeds={new Set(["circle", "following"])}
        />
      </SidebarProvider>,
    );
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(screen.getByRole("tab", { name: "Following" })).toBeDefined();
    expect(screen.queryByRole("tab", { name: "Your Circle" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Finance, Beta" })).toBeNull();
    rerender(
      <SidebarProvider>
        <PublicationTabs
          activeTab={null}
          onTabChange={() => undefined}
          visibleFeeds={new Set()}
        />
      </SidebarProvider>,
    );
    expect(screen.queryByRole("tablist")).toBeNull();
  });

  it("offers only Mark All As Read on top-level feed context menus", async () => {
    render(
      <SidebarProvider>
        <PublicationTabs activeTab="subscribed" onTabChange={() => undefined} />
      </SidebarProvider>,
    );

    const subscribed = screen.getByRole("tab", { name: "Subscribed" });
    expect(
      subscribed.querySelector("svg")?.classList.contains("lucide-newspaper"),
    ).toBe(true);
    fireEvent.contextMenu(subscribed);

    expect(await screen.findByText("Mark All As Read")).toBeDefined();
    expect(screen.queryByText("Mark All As Unread")).toBeNull();
  });

  it("does not render retired Wire or Circle tabs even with active state and stale enabled preferences", () => {
    const onWireSelect = mock(() => undefined), onCircleSelect = mock(() => undefined), onTabChange = mock((tab: "subscribed" | "following") => tab);
    render(<SidebarProvider><PublicationTabs activeTab={null} onTabChange={onTabChange}
      visibleFeeds={new Set(["wire", "circle", "subscribed", "following"])}
      wireActive circleActive onWireSelect={onWireSelect} onCircleSelect={onCircleSelect} /></SidebarProvider>);
    expect(screen.queryByRole("tab", { name: "The Wire" })).toBeNull();
    expect(screen.queryByRole("tab", { name: "Your Circle" })).toBeNull();
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    fireEvent.click(screen.getByRole("tab", { name: "Subscribed" }));
    fireEvent.click(screen.getByRole("tab", { name: "Following" }));
    expect(onTabChange.mock.calls).toEqual([["subscribed"], ["following"]]);
    expect(onWireSelect).not.toHaveBeenCalled(); expect(onCircleSelect).not.toHaveBeenCalled();
  });

  it("hides News when stale visibility only includes retired feeds", () => {
    render(<SidebarProvider><PublicationTabs activeTab={null} onTabChange={() => undefined} visibleFeeds={new Set(["wire", "circle"])} /></SidebarProvider>);
    expect(screen.queryByRole("tablist", { name: "Publication Source" })).toBeNull();
    expect(screen.queryByText("News")).toBeNull();
  });

});

test("Finance belongs to Topics with its icon, Beta badge, and selection behavior", () => {
  const onFinanceSelect = mock(() => undefined);
  const { rerender } = render(
    <SidebarProvider>
      <PublicationTabs activeTab={null} onTabChange={() => undefined} />
      <SidebarTopicsSection financeActive onFinanceSelect={onFinanceSelect} />
    </SidebarProvider>,
  );
  const feeds = screen.getByRole("tablist", { name: "Publication Source" });
  const topics = screen.getByRole("tablist", { name: "Topics" });
  const finance = screen.getByRole("tab", { name: "Finance, Beta" });
  expect(feeds.contains(finance)).toBe(false);
  expect(topics.contains(finance)).toBe(true);
  expect(screen.getByText("Topics")).toBeDefined();
  expect(finance.getAttribute("aria-selected")).toBe("true");
  expect(
    finance
      .querySelector("svg")
      ?.classList.contains("lucide-chart-no-axes-combined"),
  ).toBe(true);
  expect(finance.querySelector("span.ml-auto")?.textContent).toBe("Beta");
  fireEvent.click(finance);
  expect(onFinanceSelect).toHaveBeenCalledTimes(1);
  rerender(
    <SidebarProvider>
      <SidebarTopicsSection />
    </SidebarProvider>,
  );
  expect(
    screen
      .getByRole("tab", { name: "Finance, Beta" })
      .getAttribute("aria-selected"),
  ).toBe("false");
});

test("hides empty Topics and News sections independently", () => {
  const { rerender } = render(
    <SidebarProvider>
      <PublicationTabs
        activeTab={null}
        onTabChange={() => undefined}
        visibleFeeds={new Set(["finance"])}
      />
      <SidebarTopicsSection visibleFeeds={new Set(["finance"])} />
    </SidebarProvider>,
  );
  expect(screen.queryByText("News")).toBeNull();
  expect(screen.getByRole("tablist", { name: "Topics" })).toBeDefined();
  rerender(
    <SidebarProvider>
      <PublicationTabs
        activeTab={null}
        onTabChange={() => undefined}
        visibleFeeds={new Set(["subscribed"])}
      />
      <SidebarTopicsSection visibleFeeds={new Set(["subscribed"])} />
    </SidebarProvider>,
  );
  expect(screen.getByText("News")).toBeDefined();
  expect(screen.queryByText("Topics")).toBeNull();
  expect(screen.queryByRole("tab", { name: "Finance, Beta" })).toBeNull();
});

test("keeps active feed destinations visible before catalogs load without resurrecting retired feeds", () => {
  render(
    <SidebarProvider>
      <PublicationTabs activeTab="subscribed" onTabChange={() => undefined} />
      <SidebarTopicsSection />
    </SidebarProvider>,
  );
  for (const name of [
    "Finance, Beta",
    "Subscribed",
    "Following",
  ]) {
    expect(screen.getByRole("tab", { name })).toBeDefined();
  }
  expect(screen.queryByRole("tab", { name: "The Wire" })).toBeNull();
  expect(screen.queryByRole("tab", { name: "Your Circle" })).toBeNull();
});

test("keeps unread counts aligned inside the feed buttons", () => {
  render(
    <SidebarProvider>
      <PublicationTabs
        activeTab="subscribed"
        onTabChange={() => undefined}
        subscribedUnread={653}
        followingUnread={42}
      />
    </SidebarProvider>,
  );
  const subscribed = screen.getByRole("tab", { name: /^Subscribed/ });
  const count = screen.getByLabelText("653 unread");
  expect(subscribed.contains(count)).toBe(true);
  expect(count.className).toContain("ml-auto");
  expect(count.className).not.toContain("absolute");
});
