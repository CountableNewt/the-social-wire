import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import * as Navigation from "next/navigation";
import { SidebarTopicInterestLinks } from "@/components/AppSidebar/SidebarTopicInterestLinks";

let restore: { mockRestore: () => void }[] = [];
beforeEach(() => {
  restore = [
    spyOn(Navigation, "usePathname").mockReturnValue("/read"),
    spyOn(Navigation, "useSearchParams").mockReturnValue(new URLSearchParams("feed=finance&financeFeed=apple") as ReturnType<typeof Navigation.useSearchParams>),
  ];
});
afterEach(() => { cleanup(); for (const fn of restore) fn.mockRestore(); });
const rows = [{ id: "apple", title: "$AAPL · Apple" }];
it("links a selected interest and preserves modified browser navigation", () => {
  const changed: string[] = [];
  render(<><SidebarTopicInterestLinks topic="finance" rows={rows} loading={false} error={false} onNavigate={href => changed.push(href)} /></>);
  const link = screen.getByRole("link", { name: "$AAPL · Apple" });
  expect(link.getAttribute("href")).toBe("/read?feed=finance&financeFeed=apple");
  expect(link.getAttribute("aria-current")).toBe("page");
  fireEvent.click(link, { ctrlKey: true });
  expect(changed).toEqual([]);
  fireEvent.click(link);
  expect(changed).toEqual(["/read?feed=finance&financeFeed=apple"]);
});
it("distinguishes pending and failed interests from an empty collection", () => {
  const view = render(<><SidebarTopicInterestLinks topic="sports" rows={[]} loading error={false} /></>);
  expect(screen.getByRole("status").textContent).toBe("Loading Your Interests…");
  expect(screen.queryByText("Follow Interests in Customize.")).toBeNull();
  view.rerender(<><SidebarTopicInterestLinks topic="sports" rows={[]} loading={false} error /></>);
  expect(screen.getByRole("status").textContent).toContain("could not load");
  view.rerender(<><SidebarTopicInterestLinks topic="sports" rows={[]} loading={false} error={false} /></>);
  expect(screen.getByText("Follow Interests in Customize.")).toBeTruthy();
});
