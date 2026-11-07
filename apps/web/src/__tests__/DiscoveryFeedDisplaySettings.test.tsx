import { afterEach, beforeAll, describe, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { DiscoveryFeedDisplaySettings } from "@/components/Account/DiscoveryFeedDisplaySettings";

beforeAll(() => {
  for (const name of ["HTMLElement", "HTMLInputElement", "Element", "Node", "PointerEvent"] as const) {
    Object.defineProperty(globalThis, name, { configurable: true, value: window[name] });
  }
});
afterEach(cleanup);

describe("discovery feed display settings", () => {
  it("shows visibility switches and preserves each explicit choice", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showFinance: true, showSports: true, showSocial: true, showPodcasts: false }} isPending={false} onVisibilityChange={onVisibilityChange} />);
    expect(screen.queryByText("Always Visible")).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show The Wire" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Your Circle" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show Finance" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("finance", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Sports" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("sports", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Social" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("social", false);
    fireEvent.click(screen.getByRole("switch", { name: "Show Podcasts" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("podcasts", true);
    expect(screen.getAllByLabelText("Unread Count Not Available")).toHaveLength(4);
  });

  it("disables every active discovery control while preferences are saving", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showFinance: true, showSports: true, showSocial: true, showPodcasts: true }} isPending={true} onVisibilityChange={onVisibilityChange} />);
    for (const label of ["Finance", "Sports", "Social", "Podcasts"]) {
      const control = screen.getByRole("switch", { name: `Show ${label}` });
      expect(control.getAttribute("aria-disabled")).toBe("true");
      fireEvent.click(control);
    }
    expect(onVisibilityChange).not.toHaveBeenCalled();
  });
});
