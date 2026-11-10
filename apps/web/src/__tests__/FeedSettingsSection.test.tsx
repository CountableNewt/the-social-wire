import { afterEach, beforeAll, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { FeedSettingsSection } from "@/components/Account/FeedSettingsSection";
import * as Appearance from "@/components/Account/AppearanceSettingsSection";
import * as ReadLater from "@/components/Account/ReadLaterSettingsSection";
import * as PDSReadState from "@/components/Account/PDSReadStateSettingsSection";
import * as FeedPreferences from "@/hooks/useFeedDisplayPreferences";
import * as ReadLaterPreferences from "@/hooks/useReadLaterPreferences";
import { DEFAULT_FEED_DISPLAY_PREFERENCES } from "@/lib/feedPreferences";
import { findReadLaterService } from "@/lib/readLaterServices";
import * as PodcastPlayback from "@/lib/podcasts/playback";

const setRssArticleOpenInReader = mock((_visible: boolean) => undefined);
const setFeedVisible = mock(() => undefined);
const setFeedUnreadCountVisible = mock(() => undefined);

beforeAll(() => {
  for (const name of ["HTMLElement", "HTMLInputElement", "Element", "Node", "PointerEvent"] as const) {
    Object.defineProperty(globalThis, name, { configurable: true, value: window[name] });
  }
});

beforeEach(() => {
  setRssArticleOpenInReader.mockClear();
  spyOn(PodcastPlayback, "podcastsEnabled").mockReturnValue(true);
  setFeedVisible.mockClear();
  setFeedUnreadCountVisible.mockClear();
  spyOn(Appearance, "AppearanceSettingsSection").mockImplementation(() => <></>);
  spyOn(ReadLater, "ReadLaterSettingsSection").mockImplementation(() => <></>);
  spyOn(PDSReadState, "PDSReadStateSettingsSection").mockImplementation(() => <></>);
  spyOn(ReadLaterPreferences, "useConfiguredReadLaterService").mockReturnValue({
    isLoading: false, data: null, serviceId: "latr-link",
    service: findReadLaterService("latr-link"), sembleConnection: null,
  });
  spyOn(FeedPreferences, "useFeedDisplayPreferences").mockReturnValue({
    preferences: {
      ...DEFAULT_FEED_DISPLAY_PREFERENCES,
      rssArticleOpenMode: "reader",
      showWire: true, showCircle: true, showFinance: false,
      visibleFeeds: ["readLater", "archive"],
    },
    setFeedVisible, setFeedUnreadCountVisible,
    setDiscoveryFeedVisible: () => undefined,
    setRssArticleOpenInReader,
    setHideSportsScores: () => undefined,
    setHideFinanceCrypto: () => undefined,
    setHideFinancePerformance: () => undefined,
    isLoading: false,
    isPending: false, error: null,
  });
});

afterEach(() => {
  cleanup();
  mock.restore();
});

test("active feeds expose independent visibility controls without retired products", () => {
  render(<FeedSettingsSection />);
  expect(screen.queryByText("Always Visible")).toBeNull();
  for (const label of ["Social", "Podcasts", "Subscribed", "Following"]) {
    expect(screen.getByRole("switch", { name: `Show ${label}` })).toBeDefined();
  }
  expect(screen.queryByRole("switch", { name: "Show The Wire" })).toBeNull();
  expect(screen.queryByRole("switch", { name: "Show Your Circle" })).toBeNull();
  expect(screen.queryByRole("switch", { name: "Show Finance" })).toBeNull();
  expect(screen.queryByRole("switch", { name: "Show Sports" })).toBeNull();
  expect(screen.queryByText("Finance")).toBeNull();
  expect(screen.queryByText("Sports")).toBeNull();
  expect(screen.queryByText("The Wire")).toBeNull();
  expect(screen.queryByText("Your Circle")).toBeNull();
  fireEvent.click(screen.getByRole("switch", { name: "Show Read Later" }));
  expect(setFeedVisible).toHaveBeenCalledWith("readLater", false);
  fireEvent.click(screen.getByRole("switch", { name: "Show Archive" }));
  expect(setFeedVisible).toHaveBeenCalledWith("archive", false);
});

test("disables counts for explicitly hidden feeds", () => {
  render(<FeedSettingsSection />);
  fireEvent.click(screen.getByRole("switch", { name: "Show Subscribed Count" }));
  expect(setFeedUnreadCountVisible).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("switch", { name: "Show Following Count" }));
  expect(setFeedUnreadCountVisible).not.toHaveBeenCalled();
});

test("retired Finance options are absent while RSS reader preferences remain available", () => {
  render(<FeedSettingsSection />);
  expect(screen.queryByRole("switch", { name: "Hide Crypto in Finance" })).toBeNull();
  expect(screen.queryByText("Exclude cryptocurrency stories and interests from Finance.")).toBeNull();
  fireEvent.click(screen.getByRole("switch", { name: "Open RSS Articles in Reader" }));
  expect(setRssArticleOpenInReader).toHaveBeenCalledTimes(1);
  expect(setRssArticleOpenInReader.mock.calls[0]?.[0]).toBe(false);
});
