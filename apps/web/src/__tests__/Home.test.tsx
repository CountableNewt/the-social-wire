import { afterEach, describe, expect, it } from "bun:test";
import { cleanup, render, screen, within } from "@testing-library/react";
import Home from "@/app/page";
afterEach(() => {
  cleanup();
});
describe("Landing page feature availability", () => {
  it("features the live reader and lists without retired podcasts or topics", () => {
    render(<Home />);
    const cards = screen.getAllByRole("article");
    expect(cards.map(card => within(card).getByRole("heading").textContent)).toEqual([
      "Follow Publications", "Read Later", "Stay Organized", "Lists",
    ]);
    expect(document.body.textContent).not.toMatch(/Finance|Sports|topic/i);
    expect(screen.getByText(/Create or follow lists of publications and creators/)).toBeTruthy();
    expect(document.body.textContent).not.toMatch(/podcast/i);
    expect(screen.getAllByRole("link").some(link => link.getAttribute("href") === "/podcasts")).toBe(false);
    expect(screen.getByRole("link", { name: "Continue with ATProto" }).getAttribute("href")).toBe("/login");
    expect(document.body.textContent).not.toContain("without duplicating rules");
  });
});
