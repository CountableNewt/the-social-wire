import { afterEach, describe, expect, it } from "bun:test";
import { cleanup, render, screen, within } from "@testing-library/react";
import Home from "@/app/page";
const originalAlgorithmicFlag = process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
const originalFlag = process.env.NEXT_PUBLIC_PODCASTS_ENABLED;
afterEach(() => {
  if (originalAlgorithmicFlag === undefined) delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
  else process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = originalAlgorithmicFlag;
  cleanup();
  if (originalFlag === undefined) delete process.env.NEXT_PUBLIC_PODCASTS_ENABLED;
  else process.env.NEXT_PUBLIC_PODCASTS_ENABLED = originalFlag;
});
describe("Landing page feature availability", () => {
  it("features the live reader, topics, lists and enabled podcast listener", () => {
    process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "true";
    process.env.NEXT_PUBLIC_PODCASTS_ENABLED = "true";
    render(<Home />);
    const cards = screen.getAllByRole("article");
    expect(cards).toHaveLength(6);
    expect(cards.map(card => within(card).getByRole("heading").textContent)).toEqual([
      "Follow Publications", "Read Later", "Stay Organized", "Topics", "Lists", "Podcasts",
    ]);
    expect(screen.getByText(/Follow Finance and Sports news/)).toBeTruthy();
    expect(screen.getByText(/Create or follow lists of publications and creators/)).toBeTruthy();
    expect(screen.getByRole("link", { name: "Start Listening" }).getAttribute("href")).toBe("/podcasts");
    expect(within(cards[5]).getByRole("link", { name: "Explore Podcasts" }).getAttribute("href")).toBe("/podcasts");
    expect(screen.queryByText("In Development", { exact: true })).toBeNull();
  });
  it("keeps podcasts visible as a development preview without linking to the disabled listener", () => {
    process.env.NEXT_PUBLIC_PODCASTS_ENABLED = "false";
    render(<Home />);
    const card = screen.getAllByRole("article").find(item => within(item).queryByRole("heading", { name: "Podcasts" }))!;
    expect(within(card).getByText("In Development", { exact: true })).toBeTruthy();
    expect(within(card).getByText(/Podcast listening is in development/)).toBeTruthy();
    expect(within(card).queryByRole("link")).toBeNull();
    expect(screen.getAllByRole("link").some(link => link.getAttribute("href") === "/podcasts")).toBe(false);
    expect(screen.getByRole("link", { name: "Continue with ATProto" }).getAttribute("href")).toBe("/login");
    expect(document.body.textContent).not.toContain("without duplicating rules");
  });
});

it("does not advertise suspended topics", () => {
  delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
  render(<Home />);
  expect(screen.queryByRole("heading", { name: "Topics" })).toBeNull();
  expect(screen.getByRole("heading", { name: "Follow Publications" })).toBeDefined();
});
