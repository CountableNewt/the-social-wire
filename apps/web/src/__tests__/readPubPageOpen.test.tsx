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
let restorePreferencesSpy: (() => void) | undefined;
let automaticallyMarkAsRead: boolean | undefined = true;
let automaticReadReady = true;
let rssArticleOpenMode: "original" | "reader" = "original";
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
  automaticallyMarkAsRead = true;
  automaticReadReady = true;
  rssArticleOpenMode = "original";
  const preferencesSpy = spyOn(FeedPreferences, "useFeedDisplayPreferences").mockImplementation(() => ({ preferences: { ...DEFAULT_FEED_DISPLAY_PREFERENCES, automaticallyMarkAsRead, rssArticleOpenMode }, automaticReadReady }) as ReturnType<typeof FeedPreferences.useFeedDisplayPreferences>);
  restorePreferencesSpy = () => preferencesSpy.mockRestore();
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
  restorePreferencesSpy?.();
  restorePreferencesSpy = undefined;
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

describe("automatic article read setting", () => {
  const first = { ...unresolvedEntry, originalUrl: "https://example.com/first" };
  const second = { ...first, entryId: "at://did:plc:alice/site.standard.document/entry-2", originalUrl: "https://example.com/second" };
  const publication = "at://did:plc:alice/site.standard.publication/main";
  const stored = (id: string) => JSON.parse(window.localStorage.getItem(READ_STATE_STORAGE_KEY) ?? "{}")[id];

  it.each([true, undefined])("automatically marks successful external opens when enabled or unset (%s)", enabled => {
    automaticallyMarkAsRead = enabled;
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(true);
      expect(screen.getByTestId("entry-row").getAttribute("aria-pressed")).toBe("false");
    } finally { restore(); }
  });
  it("never automatically marks while disabled, including outside clicks, page departure, or another open", async () => {
    automaticallyMarkAsRead = false;
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      await act(async () => { window.dispatchEvent(new window.Event("focus")); document.body.click(); window.dispatchEvent(new window.Event("pagehide")); });
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      renderedEntry = second;
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(second.entryId)).toBe(false);
      page.rerender(<ReadPubPage pubId="another-publication" />);
      page.unmount();
      expect(stored(first.entryId)).toBeUndefined();
      expect(stored(second.entryId)).toBeUndefined();
    } finally { restore(); }
  });
  it("waits for known preferences before applying the default automatic behavior", () => {
    automaticReadReady = false;
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      act(() => renderedEntryListProps.markEntryRead!(first.entryId));
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(true);
    } finally { restore(); }
  });
  it("keeps explicit mark-as-read and unread actions available when disabled", () => {
    automaticallyMarkAsRead = false;
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      act(() => renderedEntryListProps.markEntryRead!(first.entryId));
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(true);
      act(() => renderedEntryListProps.markEntryUnread!(first.entryId));
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      page.unmount();
      expect(stored(first.entryId)).toBeUndefined();
    } finally { restore(); }
  });
  it.each([true, false])("applies the setting to RSS reader opens and does not mark on close (%s)", enabled => {
    automaticallyMarkAsRead = enabled;
    rssArticleOpenMode = "reader";
    renderedEntry = { ...first, entryId: "rssentry:one" };
    const reader = spyOn(RssReaderModule, "RssArticleReaderDialog").mockImplementation(({ open, onClose }) => <>{open ? <button onClick={onClose}>Close Reader</button> : null}</>);
    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(renderedEntry.entryId)).toBe(enabled);
      if (enabled) act(() => renderedEntryListProps.markEntryUnread!(renderedEntry.entryId));
      act(() => screen.getByRole("button", { name: "Close Reader" }).click());
      expect(renderedEntryListProps.isEntryRead!(renderedEntry.entryId)).toBe(false);
    } finally { reader.mockRestore(); }
  });
  it("can re-enable automatic marking for subsequent opens", () => {
    automaticallyMarkAsRead = false;
    renderedEntry = first;
    const { restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
      automaticallyMarkAsRead = true;
      page.rerender(<ReadPubPage pubId={publication} />);
      renderedEntry = second;
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.isEntryRead!(second.entryId)).toBe(true);
      expect(renderedEntryListProps.isEntryRead!(first.entryId)).toBe(false);
    } finally { restore(); }
  });
  it("honors disabling automatic marking while a PDS URL is still resolving", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      renderedEntry = unresolvedEntry;
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      automaticallyMarkAsRead = false;
      page.rerender(<ReadPubPage pubId={publication} />);
      await act(async () => resolve("https://example.com/resolved"));
      expect(tabs[0]?.location.href).toBe("https://example.com/resolved");
      expect(renderedEntryListProps.isEntryRead!(unresolvedEntry.entryId)).toBe(false);
      page.unmount();
      expect(stored(unresolvedEntry.entryId)).toBeUndefined();
    } finally { restore(); }
  });
  it("preserves an explicit unread choice made during delayed URL resolution", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { restore } = stubWindowOpen();
    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());
      act(() => renderedEntryListProps.markEntryUnread!(unresolvedEntry.entryId));
      await act(async () => resolve("https://example.com/resolved"));
      expect(renderedEntryListProps.isEntryRead!(unresolvedEntry.entryId)).toBe(false);
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
      expect(stored(first.entryId)).toBeUndefined();
    } finally { popup.mockRestore(); }
  });
  it.each(["account", "session"])("rejects delayed automatic marks after a %s change", async change => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      if (change === "account") viewerDid = "did:plc:other";
      else oauthSessionReloadSeq++;
      page.rerender(<ReadPubPage pubId={publication} />);
      await act(async () => resolve("https://example.com/stale"));
      expect(tabs[0]?.closed).toBe(true);
      expect(stored(unresolvedEntry.entryId)).toBeUndefined();
    } finally { restore(); }
  });
  it("closes obsolete resolutions without replacing the newly opened article", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      renderPage();
      act(() => screen.getByTestId("entry-row").click());
      renderedEntry = second;
      act(() => screen.getByTestId("entry-row").click());
      expect(renderedEntryListProps.resolvingEntryId).toBeNull();
      await act(async () => resolve("https://example.com/obsolete"));
      expect(tabs[0]?.closed).toBe(true);
      expect(tabs[0]?.location.href).toBe("");
      expect(renderedEntryListProps.isEntryRead!(unresolvedEntry.entryId)).toBe(false);
      expect(renderedEntryListProps.isEntryRead!(second.entryId)).toBe(true);
    } finally { restore(); }
  });
  it("does not mark a pending tab closed during URL resolution", async () => {
    let resolve: (url: string) => void = () => {};
    resolveEntryOpenUrlFromPds.mockImplementation(() => new Promise<string>(done => { resolve = done; }));
    const { tabs, restore } = stubWindowOpen();
    try {
      const page = renderPage();
      act(() => screen.getByTestId("entry-row").click());
      tabs[0]!.closed = true;
      await act(async () => resolve("https://example.com/closed"));
      expect(tabs[0]?.location.href).toBe("");
      page.unmount();
      expect(stored(unresolvedEntry.entryId)).toBeUndefined();
    } finally { restore(); }
  });
});
