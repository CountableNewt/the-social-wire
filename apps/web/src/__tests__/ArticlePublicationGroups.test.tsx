import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import * as AvatarModule from "@/components/shared/Avatar";
import { ArticlePublicationGroups } from "@/components/Articles/ArticlePublicationGroups";
import { ARTICLE_COLLAPSED_PUBLICATIONS_STORAGE_KEY } from "@/lib/articles/articlePublicationCollapseStorage";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";

const did = "did:plc:writer";
const groups = [{ id: "first", name: "First Journal", items: ["Alpha"] }, { id: "second", name: "Second Journal", items: ["Beta", "Gamma"] }];
const withQueries = (ui: ReactNode) => render(<QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>);
const view = (publications: ArticlePublication[] = []) => withQueries(<ArticlePublicationGroups did={did} publications={publications} groups={groups} renderItems={items => <ul>{items.map(item => <li key={item}>{item}</li>)}</ul>} />);

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
  withQueries(<ArticlePublicationGroups did={did} publications={[]} groups={groups.slice(0, 1)} renderItems={items => items.join()} />);
  expect(screen.queryByRole("button", { name: "Collapse All" })).toBeNull();
});

it("shows the publication icon beside matching publication headers", async () => {
  const avatar = spyOn(AvatarModule, "Avatar").mockImplementation(({ src }) => <span data-testid="publication-icon" data-src={src ?? ""} />);
  try {
    view([{ uri: "first", cid: "cid", name: "First Journal", url: "https://first.test", host: "unknown", record: { icon: "https://first.test/icon.png" } }]);
    await waitFor(() => expect(screen.getByTestId("publication-icon").getAttribute("data-src")).toBe("https://first.test/icon.png"));
    expect(screen.getAllByTestId("publication-icon")).toHaveLength(1);
    expect(screen.getByRole("button", { name: /^First Journal/ })).toBeTruthy();
  } finally { avatar.mockRestore(); }
});
