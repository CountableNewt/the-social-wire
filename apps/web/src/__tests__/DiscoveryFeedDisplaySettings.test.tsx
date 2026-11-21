import { afterEach, beforeAll, describe, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { DiscoveryFeedDisplaySettings } from "@/components/Account/DiscoveryFeedDisplaySettings";

beforeAll(() => {
  for (const name of ["HTMLElement", "HTMLInputElement", "Element", "Node", "PointerEvent"] as const) {
    Object.defineProperty(globalThis, name, { configurable: true, value: window[name] });
  }
});
afterEach(() => {
  cleanup();
  mock.restore();
});

describe("discovery feed display settings", () => {
  it("shows visibility switches and preserves each explicit choice", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showSocial: true }} isPending={false} onVisibilityChange={onVisibilityChange} />);
    expect(screen.queryByText("Always Visible")).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show The Wire" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Your Circle" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Finance" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Sports" })).toBeNull();
    expect(screen.queryByRole("switch", { name: "Show Podcasts" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show Social" }));
    expect(onVisibilityChange).toHaveBeenCalledWith("social", false);
    expect(screen.getAllByLabelText("Unread Count Not Available")).toHaveLength(1);
  });

  it("disables every active discovery control while preferences are saving", () => {
    const onVisibilityChange = mock(() => undefined);
    render(<DiscoveryFeedDisplaySettings preferences={{ showSocial: true }} isPending={true} onVisibilityChange={onVisibilityChange} />);
    const control = screen.getByRole("switch", { name: "Show Social" });
    expect(control.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(control);
    expect(onVisibilityChange).not.toHaveBeenCalled();
  });
});
