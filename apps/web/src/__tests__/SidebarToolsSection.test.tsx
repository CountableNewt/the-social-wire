import * as Pro from "@/hooks/useProEntitlements";
import { beforeEach, afterAll, afterEach, beforeAll, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { SidebarToolsSection } from "@/components/AppSidebar/SidebarToolsSection";
import { SidebarProvider } from "@/components/ui/sidebar";

const originalMatchMedia = Object.getOwnPropertyDescriptor(window, "matchMedia");
beforeAll(() => { Object.defineProperty(window, "matchMedia", { configurable: true, value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
afterAll(() => { if (originalMatchMedia) Object.defineProperty(window, "matchMedia", originalMatchMedia); else Reflect.deleteProperty(window, "matchMedia"); });
const restores: (() => void)[] = [];
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });
it("groups all tool workspaces and navigates to the selected destination", async () => {
  const entitlement = spyOn(Pro, "useProEntitlements").mockReturnValue({ isPro: true } as ReturnType<typeof Pro.useProEntitlements>); restores.push(() => entitlement.mockRestore());
  const onNavigate = mock(() => {});
  await act(async () => { render(<SidebarProvider><SidebarToolsSection pathname="/livestream" onNavigate={onNavigate} /></SidebarProvider>); });
  for (const title of ["Articles", "Live Post", "Livestream", "Brand Accounts", "Content Calendar", "Analytics"]) expect(screen.getByRole("button", { name: title })).toBeTruthy();
  expect(screen.getByRole("button", { name: "Livestream" }).getAttribute("aria-current")).toBe("page");
  expect(screen.getByRole("button", { name: "Analytics" }).getAttribute("aria-current")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Analytics" }));
  expect(onNavigate).toHaveBeenCalledWith("/analytics");
});

it("marks every Tools destination as locked for Free viewers while keeping its gate reachable", async () => {
  const entitlement = spyOn(Pro, "useProEntitlements").mockReturnValue({ isPro: false } as ReturnType<typeof Pro.useProEntitlements>); restores.push(() => entitlement.mockRestore());
  const onNavigate = mock(() => {});
  await act(async () => { render(<SidebarProvider><SidebarToolsSection pathname="/social" onNavigate={onNavigate} /></SidebarProvider>); });
  expect(screen.getByText("Pro")).toBeTruthy();
  for (const title of ["Articles", "Live Post", "Livestream", "Brand Accounts", "Content Calendar", "Analytics"]) {
    const destination = screen.getByRole("button", { name: title });
    expect(destination.querySelector("svg.lucide-lock-keyhole")).toBeTruthy();
  }
  fireEvent.click(screen.getByRole("button", { name: "Analytics" }));
  expect(onNavigate).toHaveBeenCalledWith("/analytics");
});

it("keeps Articles visible in Production without launching other gated tools", async () => {
  const originalEnv = process.env.NEXT_PUBLIC_APP_ENV;
  const originalTools = process.env.NEXT_PUBLIC_TOOLS_ENABLED;
  try {
    process.env.NEXT_PUBLIC_APP_ENV = "prod";
    delete process.env.NEXT_PUBLIC_TOOLS_ENABLED;
    delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
    const entitlement = spyOn(Pro, "useProEntitlements").mockReturnValue({ isPro: false } as ReturnType<typeof Pro.useProEntitlements>); restores.push(() => entitlement.mockRestore());
    render(<SidebarProvider><SidebarToolsSection pathname="/articles" onNavigate={() => {}} /></SidebarProvider>);
    expect(screen.getByRole("button", { name: "Articles" }).querySelector("svg.lucide-lock-keyhole")).toBeTruthy();
    for (const name of ["Live Post", "Livestream", "Brand Accounts", "Content Calendar", "Analytics"]) expect(screen.queryByRole("button", { name })).toBeNull();
  } finally {
    if (originalEnv === undefined) delete process.env.NEXT_PUBLIC_APP_ENV; else process.env.NEXT_PUBLIC_APP_ENV = originalEnv;
    if (originalTools === undefined) delete process.env.NEXT_PUBLIC_TOOLS_ENABLED; else process.env.NEXT_PUBLIC_TOOLS_ENABLED = originalTools;
  }
});

it("hides Content Calendar independently when scheduling is disabled", () => {
  process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "false";
  const entitlement = spyOn(Pro, "useProEntitlements").mockReturnValue({ isPro: true } as ReturnType<typeof Pro.useProEntitlements>); restores.push(() => entitlement.mockRestore());
  render(<SidebarProvider><SidebarToolsSection pathname="/articles" onNavigate={() => {}} /></SidebarProvider>);
  expect(screen.getByRole("button", { name: "Articles" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Content Calendar" })).toBeNull();
});

let previousSchedulingFlag: string | undefined;
beforeEach(() => {
  const previousBrandFlag = process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true";
  restores.push(() => { if (previousBrandFlag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED; else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = previousBrandFlag; }); previousSchedulingFlag = process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true"; });
afterEach(() => { if (previousSchedulingFlag === undefined) delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; else process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = previousSchedulingFlag; });


it("hides Brand Accounts by default while leaving personal tools available", () => {
  delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  const entitlement = spyOn(Pro, "useProEntitlements").mockReturnValue({ isPro: true } as ReturnType<typeof Pro.useProEntitlements>); restores.push(() => entitlement.mockRestore());
  render(<SidebarProvider><SidebarToolsSection pathname="/analytics" onNavigate={() => {}} /></SidebarProvider>);
  expect(screen.queryByRole("button", { name: "Brand Accounts" })).toBeNull();
  for (const name of ["Articles", "Live Post", "Livestream", "Content Calendar", "Analytics"]) expect(screen.getByRole("button", { name })).toBeTruthy();
});
