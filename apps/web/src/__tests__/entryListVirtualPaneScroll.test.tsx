import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from "bun:test";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { EntryListVirtualPane } from "@/components/EntryList/EntryListVirtualPane";
import { AuthProvider } from "@/hooks/useAuth";
import type { EntryListItem } from "@/lib/atprotoClient";
import { clearEntryListScrollOffsetsForTests } from "@/lib/entryListScrollState";

const globalWithResizeObserver = globalThis as typeof globalThis & {
  ResizeObserver?: new (callback: ResizeObserverCallback) => ResizeObserver;
};

if (!globalWithResizeObserver.ResizeObserver) {
  globalWithResizeObserver.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

const prototype = window.HTMLElement.prototype;
const originalHeight = Object.getOwnPropertyDescriptor(prototype, "offsetHeight");
const originalScrollTo = Object.getOwnPropertyDescriptor(prototype, "scrollTo");
const rowHeights = new Map<string, number>();
beforeEach(() => {
  clearEntryListScrollOffsetsForTests();
  rowHeights.clear();
});
afterEach(cleanup);
afterAll(() => {
  if (originalHeight) Object.defineProperty(prototype, "offsetHeight", originalHeight);
  else Reflect.deleteProperty(prototype, "offsetHeight");
  if (originalScrollTo) Object.defineProperty(prototype, "scrollTo", originalScrollTo);
  else Reflect.deleteProperty(prototype, "scrollTo");
});
beforeAll(() => {
  Object.defineProperty(prototype, "offsetHeight", {
    configurable: true,
    get() { return this.hasAttribute("data-entry-list-scroll") ? 800 : rowHeights.get(this.dataset.entryId) ?? 130; },
  });
  Object.defineProperty(prototype, "scrollTo", {
    configurable: true,
    value(options: ScrollToOptions) { this.scrollTop = options.top ?? this.scrollTop; },
  });
  Object.defineProperty(globalThis, "Element", {
    configurable: true,
    value: window.Element,
  });
  Object.defineProperty(globalThis, "HTMLElement", {
    configurable: true,
    value: window.HTMLElement,
  });
});

function makeEntry(index: number): EntryListItem {
  return {
    entryId: `at://did:plc:alice/site.standard.document/${index}`,
    title: `Entry ${index}`,
    publishedAt: "2026-01-01T00:00:00.000Z",
  };
}

function renderPane(entries: EntryListItem[], scrollStateKey = "test-feed") {
  return (
    <EntryListVirtualPane
      visibleEntries={entries}
      selectedEntryId={null}
      onSelectEntry={() => {}}
      isEntryRead={() => false}
      readIndicatorsEnabled
      hasNextPage={false}
      isFetchingNextPage={false}
      fetchNextPage={() => Promise.resolve()}
      scrollStateKey={scrollStateKey}
      publicationById={new Map()}
      markEntryRead={() => {}}
      markEntryUnread={() => {}}
    />
  );
}

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <AuthProvider>{children}</AuthProvider>
      </QueryClientProvider>
    );
  };
}

describe("EntryListVirtualPane scroll stability", () => {
  for (const [name, update, expected] of [
    ["appends a page", (entries: EntryListItem[]) => [...entries, makeEntry(100)], 900],
    ["updates metadata", (entries: EntryListItem[]) => entries.map(entry => ({ ...entry, title: `${entry.title} refreshed` })), 900],
    ["prepends an article", (entries: EntryListItem[]) => [makeEntry(100), ...entries], 1038],
  ] as const) {
    it(`uses the live viewport before a pending scroll event when a refresh ${name}`, () => {
      const entries = Array.from({ length: 30 }, (_, index) => makeEntry(index));
      const { container, rerender } = render(renderPane(entries), { wrapper: createWrapper() });
      const root = container.querySelector("[data-entry-list-scroll]") as HTMLDivElement;
      root.scrollTop = 320;
      fireEvent.scroll(root);
      // Browsers update scrollTop before delivering their next scroll event.
      root.scrollTop = 900;
      rerender(renderPane(update(entries)));
      expect(root.scrollTop).toBe(expected);
    });
  }

  for (const [name, update, expected] of [
    ["removes an earlier article", (entries: EntryListItem[]) => entries.slice(1), 182],
    ["removes the visible article", (entries: EntryListItem[]) => entries.filter((_, index) => index !== 2), 182],
    ["reorders earlier articles", (entries: EntryListItem[]) => [...entries.slice(2), ...entries.slice(0, 2)], 44],
    ["appends a page", (entries: EntryListItem[]) => [...entries, makeEntry(100)], 320],
    ["updates metadata", (entries: EntryListItem[]) => entries.map(entry => ({ ...entry, title: `${entry.title} refreshed` })), 320],
  ] as const) {
    it(`preserves the article offset when a refresh ${name}`, () => {
      const entries = Array.from({ length: 20 }, (_, index) => makeEntry(index));
      const { container, rerender } = render(renderPane(entries), { wrapper: createWrapper() });
      const root = container.querySelector("[data-entry-list-scroll]") as HTMLDivElement;
      root.scrollTop = 320;
      fireEvent.scroll(root);
      rerender(renderPane(update(entries)));
      expect(root.scrollTop).toBe(expected);
    });
  }

  it("keeps the top of the feed at the top when new articles arrive", () => {
    const entries = Array.from({ length: 20 }, (_, index) => makeEntry(index));
    const { container, rerender } = render(renderPane(entries), { wrapper: createWrapper() });
    rerender(renderPane([makeEntry(100), ...entries]));
    expect(container.querySelector("[data-entry-list-scroll]")?.scrollTop).toBe(0);
  });

  it("keeps measured heights attached to articles after a prepend", () => {
    const entries = Array.from({ length: 20 }, (_, index) => makeEntry(index));
    rowHeights.set(entries[0].entryId, 260);
    const { container, rerender } = render(renderPane(entries), { wrapper: createWrapper() });
    const root = container.querySelector("[data-entry-list-scroll]") as HTMLDivElement;
    root.scrollTop = 450;
    fireEvent.scroll(root);
    rerender(renderPane([makeEntry(100), ...entries]));
    expect(root.scrollTop).toBe(588);
  });

  it("applies a newly measured prepend once while a scroll event is pending", () => {
    const entries = Array.from({ length: 20 }, (_, index) => makeEntry(index));
    const { container, rerender } = render(renderPane(entries), { wrapper: createWrapper() });
    const root = container.querySelector("[data-entry-list-scroll]") as HTMLDivElement;
    root.scrollTop = 450;
    rowHeights.set(makeEntry(100).entryId, 260);
    rerender(renderPane([makeEntry(100), ...entries]));
    expect(root.scrollTop).toBe(718);
  });

  it("preserves the visible article offset when a refresh prepends entries", () => {
    const Wrapper = createWrapper();
    const initialEntries = Array.from({ length: 12 }, (_, index) =>
      makeEntry(index)
    );
    const { container, rerender } = render(renderPane(initialEntries), {
      wrapper: Wrapper,
    });
    const scrollRoot = container.querySelector(
      "[data-entry-list-scroll]"
    ) as HTMLDivElement;

    scrollRoot.scrollTop = 320;
    fireEvent.scroll(scrollRoot);

    rerender(renderPane([makeEntry(100), ...initialEntries]));

    expect(scrollRoot.scrollTop).toBe(458);
  });

  it("restores a feed scroll offset after the pane remounts", () => {
    clearEntryListScrollOffsetsForTests();
    const Wrapper = createWrapper();
    const entries = Array.from({ length: 12 }, (_, index) => makeEntry(index));
    const first = render(renderPane(entries, "subscribed:all"), {
      wrapper: Wrapper,
    });
    const firstRoot = first.container.querySelector(
      "[data-entry-list-scroll]"
    ) as HTMLDivElement;
    firstRoot.scrollTop = 480;
    fireEvent.scroll(firstRoot);
    first.unmount();

    const second = render(renderPane(entries, "subscribed:all"), {
      wrapper: Wrapper,
    });
    const restoredRoot = second.container.querySelector(
      "[data-entry-list-scroll]"
    ) as HTMLDivElement;
    expect(restoredRoot.scrollTop).toBe(480);
  });

  it("renders the saved viewport immediately when returning deep into a feed", () => {
    const entries = Array.from({ length: 100 }, (_, index) => makeEntry(index));
    const first = render(renderPane(entries, "deep-feed"), { wrapper: createWrapper() });
    const root = first.container.querySelector("[data-entry-list-scroll]") as HTMLDivElement;
    root.scrollTop = 5500;
    fireEvent.scroll(root);
    first.unmount();
    const returned = render(renderPane(entries, "deep-feed"), { wrapper: createWrapper() });
    expect(returned.container.querySelector("[data-entry-list-scroll]")?.scrollTop).toBe(5500);
    expect(returned.container.querySelector(`[data-entry-id="${entries[40].entryId}"]`)).not.toBeNull();
    expect(returned.container.querySelector(`[data-entry-id="${entries[0].entryId}"]`)).toBeNull();
  });
});
