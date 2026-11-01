import { afterEach, beforeAll, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import * as algorithmicFeeds from "@/lib/algorithmicFeeds";
import { DiscoveryFeedDisplaySettings } from "@/components/Account/DiscoveryFeedDisplaySettings";

beforeAll(() => {
  for (const name of ["HTMLElement", "HTMLInputElement", "Element", "Node", "PointerEvent"] as const) {
    Object.defineProperty(globalThis, name, { configurable: true, value: window[name] });
  }
});
afterEach(() => { cleanup(); mock.restore(); });

describe("discovery feed display settings", () => {
  it("hides suspended feed settings without changing stored preferences", () => {
    spyOn(algorithmicFeeds, "algorithmicFeedsEnabled").mockReturnValue(false);
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showWire: true, showCircle: true, showFinance: true, showSports: true, showSocial: true, showPodcasts: false }} isPending={false} onVisibilityChange={onVisibilityChange} />);
    for (const label of ["The Wire", "Your Circle", "Finance", "Sports"]) expect(screen.queryByText(label)).toBeNull();
    expect(screen.getByRole("switch", { name: "Show Social" })).toBeDefined();
    expect(onVisibilityChange).not.toHaveBeenCalled();
  });
  it("shows visibility switches and preserves each explicit choice", () => {
    spyOn(algorithmicFeeds, "algorithmicFeedsEnabled").mockReturnValue(true);
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showWire: true, showCircle: false, showFinance: true, showSports: true, showSocial: true, showPodcasts: false }} isPending={false} onVisibilityChange={onVisibilityChange} />);
    expect(screen.queryByText("Always Visible")).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show The Wire" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("wire", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Your Circle" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("circle", true);
    fireEvent.click(screen.getByRole("switch", { name: "Show Finance" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("finance", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Social" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("social", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Podcasts" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("podcasts", true);
    expect(screen.getAllByLabelText("Unread Count Not Available")).toHaveLength(6);
  });
});
