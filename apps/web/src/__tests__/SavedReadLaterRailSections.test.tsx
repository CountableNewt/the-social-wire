import { afterAll, afterEach, beforeAll, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { SavedCategoryFilterSection } from "@/components/AppSidebar/SavedCategoryFilterSection";
import { SavedQuickStatsSection } from "@/components/AppSidebar/SavedQuickStatsSection";
import { SidebarProvider } from "@/components/ui/sidebar";

const originalMatchMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
beforeAll(() => { Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
afterAll(() => { if (originalMatchMedia) Object.defineProperty(window, "matchMedia", originalMatchMedia); else Reflect.deleteProperty(window, "matchMedia"); });
afterEach(() => { cleanup(); });

it("shows Read Later unread and archive counts", () => {
  render(<SidebarProvider><SavedQuickStatsSection unreadCount={12} archiveCount={30} /></SidebarProvider>);
  const stats = screen.getByLabelText("Read Later Quick Stats");
  expect(stats.textContent).toContain("Unread12");
  expect(stats.textContent).toContain("Archive30");
});

it("lists every type with its count and selects a filter", () => {
  const onSelectCategory = mock(() => {});
  render(
    <SidebarProvider>
      <SavedCategoryFilterSection
        totalCount={14}
        categoryCounts={{ articles: 12, social: 2, other: 0 }}
        selectedCategory={null}
        onSelectCategory={onSelectCategory}
      />
    </SidebarProvider>,
  );
  expect(screen.getByRole("button", { name: "All Items 14" }).hasAttribute("data-active")).toBe(true);
  expect(screen.getByRole("button", { name: "Articles 12" }).hasAttribute("data-active")).toBe(false);
  expect(screen.getByRole("button", { name: "Articles 12" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Other 0" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Social 2" }));
  expect(onSelectCategory).toHaveBeenCalledWith("social");
  fireEvent.click(screen.getByRole("button", { name: "All Items 14" }));
  expect(onSelectCategory).toHaveBeenCalledWith(null);
});
