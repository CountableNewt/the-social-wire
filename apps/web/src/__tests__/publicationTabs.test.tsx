import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  mock,
  spyOn,
} from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { PublicationTabs } from "@/components/AppSidebar/PublicationTabs";
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

  it("shows only active feeds by default and honors the visibility set", () => {
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
          visibleFeeds={new Set(["following"])}
        />
      </SidebarProvider>,
    );
    expect(screen.getAllByRole("tab")).toHaveLength(1);
    expect(screen.getByRole("tab", { name: "Following" })).toBeDefined();
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
});
