import { afterEach, beforeAll, describe, expect, it } from "bun:test";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { AuthProvider } from "@/hooks/useAuth";
import type { EntryListItem } from "@/lib/atprotoClient";

beforeAll(() => {
  Object.defineProperty(globalThis, "Element", {
    configurable: true,
    value: window.Element,
  });
  Object.defineProperty(globalThis, "HTMLElement", {
    configurable: true,
    value: window.HTMLElement,
  });
});

afterEach(cleanup);

describe("EntryRow", () => {
  it("attaches the full AT URI as non-visible card metadata", async () => {
    const { EntryRow } = await import("@/components/EntryList/EntryRow");
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={queryClient}>
          <AuthProvider>{children}</AuthProvider>
        </QueryClientProvider>
      );
    }
    const entry: EntryListItem = {
      entryId: "at://did:plc:alice/site.standard.document/entry-1",
      title: "Entry",
      publishedAt: "2026-01-01T00:00:00.000Z",
    };

    const { container, queryByText } = render(
      <EntryRow
        entry={entry}
        isSelected={false}
        onSelect={() => {}}
        isRead={false}
        readIndicatorsEnabled
        onMarkEntryRead={() => {}}
        onMarkEntryUnread={() => {}}
      />,
      { wrapper: Wrapper }
    );

    expect(container.querySelector("[data-at-uri]")?.getAttribute("data-at-uri"))
      .toBe(entry.entryId);
    expect(queryByText(entry.entryId)).toBeNull();
  });

});

it.each([{ contentLabels: undefined }, { contentLabels: ["ai-generated", "sexual"] }])("publisher badges keep reader cards selectable: %p", async ({ contentLabels }) => {
  const { EntryRow } = await import("@/components/EntryList/EntryRow");
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const entry: EntryListItem = { entryId: "at://did:plc:publisher/site.standard.document/labelled", title: "Visible Card", summary: "Visible Summary", publishedAt: "2026-11-09", contentLabels: contentLabels ? [...contentLabels] : undefined };
  const selected: string[] = [];
  const { getByText, queryByText } = render(<QueryClientProvider client={queryClient}><AuthProvider><EntryRow entry={entry} isSelected={false} onSelect={id => selected.push(id)} isRead={false} readIndicatorsEnabled onMarkEntryRead={() => {}} onMarkEntryUnread={() => {}} /></AuthProvider></QueryClientProvider>);
  expect(getByText("Visible Summary")).toBeTruthy();
  expect(queryByText("AI") === null).toBe(!contentLabels);
  expect(queryByText("NSFW") === null).toBe(!contentLabels);
  fireEvent.click(getByText("Visible Card"));
  expect(selected).toEqual([entry.entryId]);
  queryClient.clear();
});
