import { afterEach, beforeAll, beforeEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { DiscoveryFeedDisplaySettings } from "@/components/Account/DiscoveryFeedDisplaySettings";
import * as PodcastPlayback from "@/lib/podcasts/playback";

beforeAll(() => {
  for (const name of ["HTMLElement", "HTMLInputElement", "Element", "Node", "PointerEvent"] as const) {
    Object.defineProperty(globalThis, name, { configurable: true, value: window[name] });
  }
});
beforeEach(() => {
  spyOn(PodcastPlayback, "podcastsEnabled").mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  mock.restore();
});

describe("discovery feed display settings", () => {
  it("shows visibility switches and preserves each explicit choice", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showWire: true, showCircle: true, showFinance: true, showSports: true, showSocial: true, showPodcasts: false }} isPending={false} onVisibilityChange={onVisibilityChange} />);
    expect(screen.queryByText("Always Visible")).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show The Wire" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Your Circle" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Finance" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Sports" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show Social" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("social", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Podcasts" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("podcasts", true);
    expect(screen.getAllByLabelText("Unread Count Not Available")).toHaveLength(2);
  });

  it("hides Podcasts settings when the Podcasts feature is disabled", () => {
    spyOn(PodcastPlayback, "podcastsEnabled").mockReturnValue(false);
    render(<DiscoveryFeedDisplaySettings preferences={{ showWire: true, showCircle: true, showFinance: true, showSports: true, showSocial: true, showPodcasts: true }} isPending={false} onVisibilityChange={mock(() => undefined)} />);
    expect(screen.queryByRole("switch", { name: "Show Podcasts" })).toBeNull();
    expect(screen.queryByText("Podcasts")).toBeNull();
    expect(screen.getByRole("switch", { name: "Show Social" })).toBeDefined();
    expect(screen.getAllByLabelText("Unread Count Not Available")).toHaveLength(1);
  });

  it("disables every active discovery control while preferences are saving", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showWire: true, showCircle: true, showFinance: true, showSports: true, showSocial: true, showPodcasts: true }} isPending={true} onVisibilityChange={onVisibilityChange} />);
    for (const label of ["Social", "Podcasts"]) {
      const control = screen.getByRole("switch", { name: `Show ${label}` });
      expect(control.getAttribute("aria-disabled")).toBe("true");
      fireEvent.click(control);
    }
    expect(onVisibilityChange).not.toHaveBeenCalled();
  });
});
