import { describe, expect, it } from "bun:test"
import { render, screen } from "@testing-library/react"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar"

describe("Sidebar", () => {
  it("keeps the toggle footer inside the viewport-height flex layout", () => {
    render(
      <SidebarProvider>
        <Sidebar>
          <SidebarContent>Navigation</SidebarContent>
          <SidebarFooter>
            <SidebarTrigger />
          </SidebarFooter>
        </Sidebar>
        <SidebarInset>Content</SidebarInset>
      </SidebarProvider>,
    )

    const aside = screen.getByRole("complementary")
    const toggle = screen.getByRole("button", { name: "Collapse Sidebar" })
    expect(aside.className).toContain("h-[calc(100svh-var(--operations-banner-height,0rem))]")
    expect(aside.className).toContain("flex-col")
    expect(toggle.parentElement?.className).toContain("shrink-0")
    expect(toggle.parentElement?.className).not.toContain("absolute")
    expect(aside.parentElement?.className).toContain("overflow-hidden")
    expect(aside.parentElement?.className).toContain("h-[calc(100svh-var(--operations-banner-height,0rem))]")
    expect(screen.getByRole("main").className).toContain("overflow-y-auto")
    expect(screen.getByRole("main").className).toContain("overscroll-contain")
  })
})
