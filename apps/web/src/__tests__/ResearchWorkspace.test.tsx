import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Semble from "@/hooks/useSembleReadLater";
import * as Margin from "@/hooks/research/useMarginAnnotations";
import { ResearchWorkspace } from "@/components/Research/ResearchWorkspace";
const restores: (() => void)[] = [];
let client: QueryClient;
const retry = mock(async () => {});
function collections(value: unknown) { const spy = spyOn(Semble, "useSembleCollections").mockReturnValue(value as ReturnType<typeof Semble.useSembleCollections>); restores.push(() => spy.mockRestore()); }
function annotations(value: unknown) { const spy = spyOn(Margin, "useMarginAnnotations").mockReturnValue(value as ReturnType<typeof Margin.useMarginAnnotations>); restores.push(() => spy.mockRestore()); }
beforeEach(() => {
  client = new QueryClient(); retry.mockClear();
  collections({ collections: [{ uri: "at://did:plc:writer/network.cosmik.collection/one", name: "Research Sources", cardCount: 1, accessType: "OPEN" }], isPending: false, error: null, hasNextPage: false, refetch: retry });
  const items = spyOn(Semble, "useSembleCollectionItems").mockReturnValue({ items: [{ id: "one", title: "Promising Source", url: "https://source.test", cardType: "URL" }], isPending: false, error: null, hasNextPage: false } as unknown as ReturnType<typeof Semble.useSembleCollectionItems>); restores.push(() => items.mockRestore());
  annotations({ data: [{ uri: "annotation", motivation: "highlighting", title: "Revisit This Essay", quote: "A Passage", body: "My Note", source: "javascript:alert(1)", tags: ["writing"], createdAt: "2026-11-01T12:00:00Z" }], isPending: false, error: null, refetch: retry });
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
function show() { render(<QueryClientProvider client={client}><ResearchWorkspace did="did:plc:writer" /></QueryClientProvider>); }
it("browses Semble and Margin while suppressing unsafe source links", () => {
  show(); expect(screen.getByRole("heading", { name: "Research Sources" })).toBeTruthy();
  expect(screen.getByRole("link", { name: "Open Source" }).getAttribute("href")).toBe("https://source.test");
  fireEvent.click(screen.getByRole("button", { name: "Margin" }));
  expect(screen.getByText("“A Passage”")).toBeTruthy(); expect(screen.getByText("My Note")).toBeTruthy();
  expect(screen.queryByRole("link", { name: "Open Source" })).toBeNull();
});
it("moves the selected source styling together with the visible research content", () => {
  show();
  const semble = screen.getByRole("button", { name: "Semble" });
  const margin = screen.getByRole("button", { name: "Margin" });
  expect(semble.classList.contains("bg-secondary")).toBe(true);
  expect(margin.classList.contains("bg-secondary")).toBe(false);
  fireEvent.click(margin);
  expect(margin.getAttribute("aria-pressed")).toBe("true");
  expect(margin.classList.contains("bg-secondary")).toBe(true);
  expect(semble.getAttribute("aria-pressed")).toBe("false");
  expect(semble.classList.contains("bg-secondary")).toBe(false);
  expect(screen.getByRole("heading", { name: "Revisit This Essay" })).toBeTruthy();
  fireEvent.click(semble);
  expect(semble.getAttribute("aria-pressed")).toBe("true");
  expect(semble.classList.contains("bg-secondary")).toBe(true);
  expect(margin.getAttribute("aria-pressed")).toBe("false");
  expect(margin.classList.contains("bg-secondary")).toBe(false);
  expect(screen.getByRole("heading", { name: "Research Sources" })).toBeTruthy();
});
it("keeps Margin usable after Semble failure and offers retry", async () => {
  collections({ collections: [], isPending: false, error: new Error("Semble Unavailable"), refetch: retry }); show();
  expect(screen.getByRole("alert").textContent).toContain("Semble Unavailable");
  fireEvent.click(screen.getByRole("button", { name: "Retry Semble" })); await waitFor(() => expect(retry).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole("button", { name: "Margin" })); expect(screen.getByRole("heading", { name: "Revisit This Essay" })).toBeTruthy();
});
it("shows loading/empty states and refreshes both source query scopes", async () => {
  collections({ collections: [], isPending: true, error: null }); annotations({ data: [], isPending: false, error: null });
  const invalidate = spyOn(client, "invalidateQueries").mockResolvedValue(); show();
  expect(screen.getByRole("status").textContent).toContain("Loading Semble");
  fireEvent.click(screen.getByRole("button", { name: "Margin" })); expect(screen.getByRole("heading", { name: "No Margin Annotations Found" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Refresh Research" })); await waitFor(() => expect(invalidate).toHaveBeenCalledTimes(2));
});
it("offers Margin retry while keeping Semble accessible", async () => {
  annotations({ data: undefined, isPending: false, error: new Error("Margin Unavailable"), refetch: retry }); show();
  fireEvent.click(screen.getByRole("button", { name: "Margin" }));
  expect(screen.getByRole("alert").textContent).toContain("Margin Unavailable");
  fireEvent.click(screen.getByRole("button", { name: "Retry Margin" }));
  await waitFor(() => expect(retry).toHaveBeenCalledTimes(1));
  fireEvent.click(screen.getByRole("button", { name: "Semble" }));
  expect(screen.getByRole("heading", { name: "Research Sources" })).toBeTruthy();
});
it("renders Margin loading and Semble empty states", () => {
  collections({ collections: [], isPending: false, error: null });
  annotations({ data: undefined, isPending: true, error: null }); show();
  expect(screen.getByRole("heading", { name: "No Semble Collections Found" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Margin" }));
  expect(screen.getByRole("status").textContent).toContain("Loading Margin");
});
