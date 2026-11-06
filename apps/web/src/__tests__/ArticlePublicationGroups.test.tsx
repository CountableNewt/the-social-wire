import { afterEach, beforeEach, expect, it } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ArticlePublicationGroups } from "@/components/Articles/ArticlePublicationGroups";
import { ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY } from "@/lib/articles/articlePublicationCollapseStorage";

const did = "did:plc:writer";
const groups = [{ id: "first", name: "First Journal", items: ["Alpha"] }, { id: "second", name: "Second Journal", items: ["Beta", "Gamma"] }];
const view = () => render(<ArticlePublicationGroups did={did} groups={groups} renderItems={items => <ul>{items.map(item => <li key={item}>{item}</li>)}</ul>} />);

beforeEach(() => window.localStorage.removeItem(ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY));
afterEach(cleanup);

it("collapses and expands a single publication and remembers it", () => {
  view();
  const header = screen.getByRole("button", { name: /First Journal/ });
  expect(header.getAttribute("aria-expanded")).toBe("true");
  fireEvent.click(header);
  expect(header.getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByText("Alpha")).toBeNull();
  expect(screen.getByText("Beta")).toBeTruthy();
  cleanup();
  view();
  expect(screen.getByRole("button", { name: /First Journal/ }).getAttribute("aria-expanded")).toBe("false");
  expect(screen.queryByText("Alpha")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: /First Journal/ }));
  expect(screen.getByText("Alpha")).toBeTruthy();
});

it("collapses and expands every publication at once", () => {
  view();
  fireEvent.click(screen.getByRole("button", { name: "Collapse All" }));
  expect(screen.queryByText("Alpha")).toBeNull();
  expect(screen.queryByText("Beta")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Expand All" }));
  expect(screen.getByText("Alpha")).toBeTruthy();
  expect(screen.getByText("Gamma")).toBeTruthy();
});

it("hides the bulk toggle for a single publication", () => {
  render(<ArticlePublicationGroups did={did} groups={groups.slice(0, 1)} renderItems={items => items.join()} />);
  expect(screen.queryByRole("button", { name: "Collapse All" })).toBeNull();
});
