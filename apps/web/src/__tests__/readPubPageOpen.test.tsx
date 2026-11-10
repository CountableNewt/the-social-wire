import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  spyOn,
} from "bun:test";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { ReadRouteProvider } from "@/contexts/ReadRouteContext";
import * as AuthHook from "@/hooks/useAuth";
import * as EntryListModule from "@/components/EntryList/EntryList";
import * as RssReaderModule from "@/components/EntryDetail/RssArticleReaderDialog";
import * as FeedPreferences from "@/hooks/useFeedDisplayPreferences";
import { DEFAULT_FEED_DISPLAY_PREFERENCES } from "@/lib/feedPreferences";
import * as WireEditionHook from "@/hooks/useWireEdition";
import type { EntryListItem } from "@/lib/atprotoClient";
import * as ResolveEntryOpenURL from "@/lib/resolveEntryOpenUrl";
import { READ_STATE_STORAGE_KEY } from "@/lib/entryReadStateStorage";
import type { WireEditionPage } from "@/lib/wireEditionClient";

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

let restoreAuthSpy: (() => void) | undefined;
let restoreEntryListSpy: (() => void) | undefined;
let viewerDid = "did:plc:viewer";
let oauthSessionReloadSeq = 0;
let resolveEntryOpenUrlFromPds: ReturnType<
  typeof spyOn<typeof ResolveEntryOpenURL, "resolveEntryOpenUrlFromPds">
>;
let useWireEdition: ReturnType<
  typeof spyOn<typeof WireEditionHook, "useWireEdition">
>;

beforeEach(() => {
  process.env.NEXT_PUBLIC_WIRE_NEWS_EDITION_ENABLED = "true";
  window.localStorage.clear();
  renderedEntry = unresolvedEntry;
  renderedEntryListProps = {};
  viewerDid = "did:plc:viewer";
  oauthSessionReloadSeq = 0;
  const entryListSpy = spyOn(EntryListModule, "EntryList").mockImplementation(renderEntryList);
  restoreEntryListSpy = () => entryListSpy.mockRestore();
  const authSpy = spyOn(AuthHook, "useAuth").mockImplementation(() => ({
    session: { did: viewerDid },
    getOAuthSession: () => null,
    oauthSessionReloadSeq,
  } as ReturnType<typeof AuthHook.useAuth>));
  restoreAuthSpy = () => authSpy.mockRestore();
  resolveEntryOpenUrlFromPds = spyOn(
    ResolveEntryOpenURL,
    "resolveEntryOpenUrlFromPds",
  ).mockResolvedValue(undefined);
  useWireEdition = spyOn(
    WireEditionHook,
    "useWireEdition",
  ).mockImplementation(
    () =>
      ({
        data: {
          pages: [wireEditionPageForEntry(renderedEntry)],
          pageParams: [undefined],
        },
        catalog: { isLoading: false },
        isLoading: false,
        isError: false,
        error: null,
        viewerModerationError: false,
        moreStories: [],
        hasNextPage: false,
        isFetchingNextPage: false,
        isFetchNextPageError: false,
        fetchNextPage: async () => undefined,
        retryTheWire: async () => undefined,
      }) as unknown as ReturnType<typeof WireEditionHook.useWireEdition>,
  );
});

afterEach(() => {
  delete process.env.NEXT_PUBLIC_WIRE_NEWS_EDITION_ENABLED;
  cleanup();
  window.localStorage.clear();
  restoreAuthSpy?.();
  restoreAuthSpy = undefined;
  restoreEntryListSpy?.();
  restoreEntryListSpy = undefined;
  resolveEntryOpenUrlFromPds.mockRestore();
  useWireEdition.mockRestore();
});

/** standard.site entry the AppView could not index a hosted URL for. */
const unresolvedEntry: EntryListItem = {
  entryId: "at://did:plc:alice/site.standard.document/entry-1",
  title: "Standard Site Article",
  publishedAt: "2026-01-01T00:00:00.000Z",
};
let renderedEntry = unresolvedEntry;
let renderedEntryListProps: {
  wireFeed?: boolean;
  readIndicatorsEnabled?: boolean;
  articleFilter?: string;
  isEntryRead?: (entryId: string) => boolean;
  markEntryRead?: (entryId: string) => void;
  markEntryUnread?: (entryId: string) => void;
  resolvingEntryId?: string | null;
} = {};

function wireEditionPageForEntry(entry: EntryListItem): WireEditionPage {
  const itemId = entry.wireItem?.itemId ?? entry.entryId;
  return {
    editionVersion: "1",
    generationId: "test-generation",
    generatedAt: "2026-08-20T12:00:00.000Z",
    language: "en",
    source: "ranked",
    degraded: false,
    stories: [
      {
        itemId,
        canonicalUrl: entry.originalUrl ?? "https://news.example/story",
        representativeUri: entry.entryId,
        title: entry.title,
        publishedAt: entry.publishedAt,
        source: entry.wireItem?.source ?? {
          name: "Example News",
          domain: "news.example",
        },
        reasons: entry.wireItem?.reasons ?? [],
        provenance: entry.wireItem?.provenance ?? [],
      },
    ],
    topStoryIds: [itemId],
    publicationSpotlights: [],
    storyRails: [],
    people: [],
    trendingStoryIds: [],
  };
}

const renderEntryList = ({
    onSelectEntry,
    resolvingEntryId,
    wireFeed,
    readIndicatorsEnabled,
    articleFilter,
    isEntryRead,
    markEntryRead,
    markEntryUnread,
    selectedEntryId,
  }: {
    onSelectEntry: (entryId: string, entry?: EntryListItem) => void;
    resolvingEntryId?: string | null;
    wireFeed?: boolean;
    readIndicatorsEnabled?: boolean;
    articleFilter?: string;
    isEntryRead?: (entryId: string) => boolean;
    markEntryRead?: (entryId: string) => void;
    markEntryUnread?: (entryId: string) => void;
    selectedEntryId?: string | null;
  }) => (
    <button
      type="button"
      data-testid="entry-row"
      data-entry-id={renderedEntry.entryId}
      aria-pressed={selectedEntryId === renderedEntry.entryId}
      aria-busy={resolvingEntryId === renderedEntry.entryId || undefined}
      onClick={() => onSelectEntry(renderedEntry.entryId, renderedEntry)}
      ref={() => {
        renderedEntryListProps = {
          wireFeed,
          readIndicatorsEnabled,
          articleFilter,
          isEntryRead,
          markEntryRead,
          markEntryUnread,
          resolvingEntryId,
        };
      }}
    >
      {renderedEntry.title}
    </button>
  );

const { default: ReadPubPage } = await import(
  "@/app/read/[...pubId]/ReadPubPage"
);

function Wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>
      <ReadRouteProvider>{children}</ReadRouteProvider>
    </QueryClientProvider>
  );
}

function renderPage() {
  return render(
    <ReadPubPage pubId="at://did:plc:alice/site.standard.publication/main" />,
    { wrapper: Wrapper },
  );
}

type OpenedTab = { closed: boolean; location: { href: string } };

function stubWindowOpen(): { tabs: OpenedTab[]; restore: () => void } {
  const original = window.open;
  const tabs: OpenedTab[] = [];
  window.open = ((_url?: string | URL, _target?: string, features?: string) => {
    const tab: OpenedTab = { closed: false, location: { href: "" } };
    tabs.push(tab);
    // Browsers return null when the features string asks for noopener/noreferrer, so a caller
    // that needs the handle to navigate the tab later must not pass them.
    if (features && /\bno(opener|referrer)\b/.test(features)) return null;
    return {
      get closed() { return tab.closed; },
      close: () => {
        tab.closed = true;
      },
      location: tab.location,
      opener: window as Window | null,
    } as unknown as Window;
  }) as typeof window.open;
  return {
    tabs,
    restore: () => {
      window.open = original;
    },
  };
}

describe("ReadPubPage entry open fallback", () => {
  it("opens the PDS-resolved URL when the AppView has no destination", async () => {
    resolveEntryOpenUrlFromPds.mockResolvedValue(
      "https://example.com/posts/hello",
    );
    const { tabs, restore } = stubWindowOpen();

    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());

      await waitFor(() => {
        expect(tabs[0]?.location.href).toBe("https://example.com/posts/hello");
      });
      // The tab is claimed synchronously so Safari does not treat it as a popup.
      expect(tabs).toHaveLength(1);
      expect(tabs[0]?.closed).toBe(false);
      expect(screen.queryByRole("status")).toBeNull();
    } finally {
      restore();
    }
  });

  it("opens The Wire stories through the isolated news surface without creating read state", async () => {
    renderedEntry = {
      entryId: "at://did:plc:writer/site.standard.document/wire-story",
      title: "The Wire Story",
      publishedAt: "2026-08-20T12:00:00.000Z",
      originalUrl: "https://news.example/story",
      wireItem: {
        itemId: "wire:item:one",
        representativeUri:
          "at://did:plc:writer/site.standard.document/wire-story",
        source: { name: "Example News", domain: "news.example" },
        reasons: ["breaking_story"],
        provenance: [],
      },
    };
    const { tabs, restore } = stubWindowOpen();

    try {
      render(
        <ReadPubPage wireFeed />,
        { wrapper: Wrapper },
      );
      const storyTitle = await screen.findByText("The Wire Story");
      const story = storyTitle.closest('[role="link"]');
      if (!story) throw new Error("Wire story card did not render");
      act(() => (story as HTMLElement).click());

      expect(renderedEntryListProps).toEqual({});
      expect(tabs).toHaveLength(1);
      expect(window.localStorage.getItem(READ_STATE_STORAGE_KEY)).toBeNull();
      expect(resolveEntryOpenUrlFromPds).not.toHaveBeenCalled();
    } finally {
      restore();
      renderedEntry = unresolvedEntry;
    }
  });

  it("keeps the existing Wire list active until the news edition rollout gate is enabled", () => {
    delete process.env.NEXT_PUBLIC_WIRE_NEWS_EDITION_ENABLED;

    render(<ReadPubPage wireFeed />, { wrapper: Wrapper });

    expect(screen.getByTestId("entry-row")).toBeDefined();
    expect(renderedEntryListProps).toMatchObject({
      wireFeed: true,
      readIndicatorsEnabled: false,
      articleFilter: "all",
    });
  });

  it("surfaces a message instead of silently doing nothing", async () => {
    resolveEntryOpenUrlFromPds.mockResolvedValue(undefined);
    const { tabs, restore } = stubWindowOpen();

    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());

      const status = await screen.findByRole("status");
      expect(status.textContent).toContain(
        "Couldn't Find A Link For This Article.",
      );
      expect(tabs[0]?.closed).toBe(true);
    } finally {
      restore();
    }
  });

  it("marks the row busy while the destination is resolving", async () => {
    let release: (url: string | undefined) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(
      () =>
        new Promise<string | undefined>((resolve) => {
          release = resolve;
        }),
    );
    const { restore } = stubWindowOpen();

    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());

      await waitFor(() => {
        expect(screen.getByTestId("entry-row").getAttribute("aria-busy")).toBe(
          "true",
        );
      });

      await act(async () => release("https://example.com/posts/hello"));
      await waitFor(() => {
        expect(
          screen.getByTestId("entry-row").getAttribute("aria-busy"),
        ).toBeNull();
      });
    } finally {
      restore();
      resolveEntryOpenUrlFromPds.mockReset();
    }
  });
});

describe("deferred article read state", () => {
  const first = { ...unresolvedEntry, originalUrl: "https://example.com/first" };
  const second = { ...first, entryId: "at://did:plc:alice/site.standard.document/entry-2", originalUrl: "https://example.com/second" };
  it("keeps an external article unread on return and marks it only when another article opens", () => {
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(screen.getByTestId("entry-row").getAttribute("aria-pressed")).toBe("true");
      act(() => { window.dispatchEvent(new window.Event("blur")); window.dispatchEvent(new window.Event("focus")); document.dispatchEvent(new window.Event("visibilitychange")); });
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      renderedEntry = second;
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(true);
      expect(renderedEntryListProps.isEntryRead!(second.entryId)).toBe(false);
      page.unmount();
    } finally { restore(); }
  });
  it("keeps the opened card highlighted until a click outside marks it read", async () => {
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(screen.getByTestId("entry-row").getAttribute("aria-pressed")).toBe("true");
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      await act(async () => document.body.click());
      expect(screen.getByTestId("entry-row").getAttribute("aria-pressed")).toBe("false");
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(true);
      page.unmount();
    } finally { restore(); }
  });
  it("durably marks the article when the entire reader provider unmounts", () => {
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      page.unmount();
      expect(JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[first.entryId]).toBeTruthy();
    } finally { restore(); }
  });
  it("honors an explicit unread action when leaving the feed", () => {
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      act(() => renderedEntryListProps.markEntryUnread!(first.entryId));
      page.unmount();
      expect(JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[first.entryId]).toBeUndefined();
    } finally { restore(); }
  });
  it("does not mark an article after a blocked popup", () => {
    renderedEntry = first;
    const popup = spyOn(window, "open").mockReturnValue(null);
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(screen.getByRole("status").textContent).toContain("Allow Pop-Ups");
      page.unmount();
      expect(JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[first.entryId]).toBeUndefined();
    } finally { popup.mockRestore(); }
  });
  it("marks a reader article only when its reader closes, even with the All filter", () => {
    renderedEntry = { ...first, entryId: "rssentry:one" };
    const reader = spyOn(RssReaderModule, "RssArticleReaderDialog").mockImplementation(({ open, onClose }) => <>{open ? <button onClick={onClose}>Close Reader</button> : null}</>);
    const preferences = spyOn(FeedPreferences, "useFeedDisplayPreferences").mockReturnValue({ preferences: { ...DEFAULT_FEED_DISPLAY_PREFERENCES, rssArticleOpenMode: "reader" } } as ReturnType<typeof FeedPreferences.useFeedDisplayPreferences>);
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(renderedEntry.entryId)).toBe(false);
      act(() => screen.getByRole("button", { name: "Close Reader" }).click());
      expect(renderedEntryListProps.isEntryRead!(renderedEntry.entryId)).toBe(true);
      page.unmount();
    } finally { reader.mockRestore(); preferences.mockRestore(); }
  });
  it.each(["account", "session"])("drops deferred read marks after a %s change", change => {
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      if (change === "account") viewerDid = "did:plc:other";
      else oauthSessionReloadSeq++;
      page.rerender(<ReadPubPage pubId="at://did:plc:alice/site.standard.publication/main" />);
      page.unmount();
      expect(JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[first.entryId]).toBeUndefined();
    } finally { restore(); }
  });
  it("closes obsolete resolved tabs without marking or replacing the newly opened article", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      renderedEntry = unresolvedEntry;
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      renderedEntry = second;
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.resolvingEntryId).toBeNull();
      await act(async () => resolve("https://example.com/obsolete"));
      expect(tabs[0]?.closed).toBe(true);
      expect(tabs[0]?.location.href).toBe("");
      expect(renderedEntryListProps.isEntryRead!(unresolvedEntry.entryId)).toBe(false);
      expect(renderedEntryListProps.isEntryRead!(second.entryId)).toBe(false);
      page.unmount();
    } finally { restore(); }
  });
  it("does not mark or navigate a pending tab that was closed during link resolution", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      renderedEntry = unresolvedEntry;
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      tabs[0]!.closed = true;
      await act(async () => resolve("https://example.com/closed"));
      expect(tabs[0]?.location.href).toBe("");
      page.unmount();
      expect(JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[unresolvedEntry.entryId]).toBeUndefined();
    } finally { restore(); }
  });
});
