import { afterAll, afterEach, beforeAll, expect, it, mock } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { SavedCategoryFilterSection } from "@/components/AppSidebar/SavedCategoryFilterSection";
import { SavedFeedSourcesSection } from "@/components/AppSidebar/SavedFeedSourcesSection";
import { SidebarProvider } from "@/components/ui/sidebar";

const originalMatchMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
beforeAll(() => { Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
afterAll(() => { if (originalMatchMedia) Object.defineProperty(window, "matchMedia", originalMatchMedia); else Reflect.deleteProperty(window, "matchMedia"); });
afterEach(() => { cleanup(); });

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

it("lists publications under one heading with an All Publications row", () => {
  const onSelectSource = mock(() => {});
  render(
    <SidebarProvider>
      <SavedFeedSourcesSection
        sources={[{ key: "site:theverge.com", name: "The Verge", count: 3 }]}
        selectedSource={null}
        onSelectSource={onSelectSource}
      />
    </SidebarProvider>,
  );
  expect(screen.getAllByText("Publications")).toHaveLength(1);
  expect(screen.getByRole("button", { name: "All Publications" }).hasAttribute("data-active")).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: /The Verge/ }));
  expect(onSelectSource).toHaveBeenCalledWith("site:theverge.com");
  fireEvent.click(screen.getByRole("button", { name: "All Publications" }));
  expect(onSelectSource).toHaveBeenCalledWith(null);
});
