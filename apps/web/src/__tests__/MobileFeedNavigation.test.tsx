import { afterAll, afterEach, beforeAll, describe, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { MobileFeedNavigation } from "@/components/AppSidebar/MobileFeedNavigation";

const browserGlobalKeys = ["requestAnimationFrame", "cancelAnimationFrame", "HTMLElement", "Element", "Node", "getComputedStyle"] as const;
const originalBrowserGlobals = new Map(browserGlobalKeys.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));

beforeAll(() => {
  const browserGlobals = {
    requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0),
    cancelAnimationFrame: (handle: ReturnType<typeof setTimeout>) => clearTimeout(handle),
    HTMLElement: window.HTMLElement,
    Element: window.Element,
    Node: window.Node,
    getComputedStyle: window.getComputedStyle.bind(window),
  };
  for (const key of browserGlobalKeys) {
    Object.defineProperty(globalThis, key, {
      configurable: true,
      writable: true,
      value: browserGlobals[key],
    });
  }
});

afterEach(cleanup);
afterAll(() => {
  for (const key of browserGlobalKeys) {
    const descriptor = originalBrowserGlobals.get(key);
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
});

describe("MobileFeedNavigation", () => {
  it("keeps Social reachable and reader Following separate after retirement", () => {
    const onSelect = mock(() => undefined);
    render(<MobileFeedNavigation currentFeed="social" visibleFeeds={new Set(["social", "subscribed", "following"])} onSelect={onSelect} />);
    expect(screen.getByRole("button", { name: "Following" }).getAttribute("aria-current")).toBeNull();
    expect(screen.queryByRole("button", { name: "More Feeds" })).toBeNull();
    const social = screen.getByRole("button", { name: "Social" });
    expect(social.getAttribute("aria-current")).toBe("page");
    fireEvent.click(social);
    expect(onSelect).toHaveBeenCalledWith("social");
  });

  it("shows visible feeds and preserves the active feed", () => {
    const onSelect = mock(() => undefined);
    render(
      <MobileFeedNavigation
        currentFeed="subscribed"
        visibleFeeds={new Set(["readLater", "subscribed", "following"])}
        onSelect={onSelect}
      />,
    );

    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
    expect(
      screen.getByRole("button", { name: "Subscribed" }).getAttribute("aria-current"),
    ).toBe("page");
    expect(
      screen
        .getByRole("button", { name: "Subscribed" })
        .querySelector("svg")
        ?.classList.contains("lucide-newspaper"),
    ).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Following" }));
    expect(onSelect).toHaveBeenCalledWith("following");
  });

  it("keeps four active primary slots and places remaining destinations in More", async () => {
    const onSelect = mock(() => undefined);
    render(<MobileFeedNavigation currentFeed="archive" visibleFeeds={new Set(["social", "readLater", "archive", "subscribed", "following"])} onSelect={onSelect} />);
    const navigation = screen.getByRole("navigation", { name: "Feed Navigation" });
    expect(within(navigation).getAllByRole("button").length).toBe(5);
    expect(screen.getByRole("button", { name: "Social" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Saved" })).toBeDefined();
    const more = screen.getByRole("button", { name: "More Feeds" });
    expect(more.getAttribute("aria-current")).toBe("page"); fireEvent.click(more);
    const archive = await screen.findByRole("menuitem", { name: "Archive" });
    expect(archive.getAttribute("aria-current")).toBe("page"); fireEvent.click(archive);
    expect(onSelect).toHaveBeenCalledWith("archive");
    await waitFor(() => expect(screen.queryByRole("menuitem", { name: "Archive" })).toBeNull());
  });

  it("uses available destinations to fill empty primary slots without a needless More menu", () => {
    render(<MobileFeedNavigation currentFeed="archive" visibleFeeds={new Set(["following", "readLater", "archive", "subscribed"])} onSelect={() => undefined} readLaterLabel="Read Later" />);
    expect(screen.getAllByRole("button").length).toBe(4);
    expect(screen.queryByRole("button", { name: "More Feeds" })).toBeNull();
    expect(screen.getByRole("button", { name: "Read Later" })).toBeDefined();
    expect(screen.getByRole("button", { name: "Archive" }).getAttribute("aria-current")).toBe("page");
  });

  it("hides the navigation when no destinations are available", () => {
    render(<MobileFeedNavigation currentFeed={null} visibleFeeds={new Set()} onSelect={() => undefined} />);
    expect(screen.queryByRole("navigation")).toBeNull();
  });

  it("opens Lists from More without replacing a primary feed slot", async () => {
    const onOpenLists = mock(() => undefined);
    render(<MobileFeedNavigation currentFeed={null} listsActive visibleFeeds={new Set(["subscribed", "following"])} onSelect={() => undefined} onOpenLists={onOpenLists} />);
    expect(screen.queryByRole("button", { name: "The Wire" })).toBeNull();
    const more = screen.getByRole("button", { name: "More Feeds" });
    expect(more.getAttribute("aria-current")).toBe("page");
    fireEvent.click(more);
    const lists = await screen.findByRole("menuitem", { name: "Lists" });
    fireEvent.click(lists);
    expect(onOpenLists).toHaveBeenCalledTimes(1);
  });

  it("keeps Lists reachable when all feeds are explicitly hidden", async () => {
    const onOpenLists = mock(() => undefined);
    render(<MobileFeedNavigation currentFeed={null} visibleFeeds={new Set()} onSelect={() => undefined} onOpenLists={onOpenLists} />);
    fireEvent.click(screen.getByRole("button", { name: "More Feeds" }));
    fireEvent.click(await screen.findByRole("menuitem", {name:"Lists"}));
    expect(onOpenLists).toHaveBeenCalledTimes(1);
  });

});
