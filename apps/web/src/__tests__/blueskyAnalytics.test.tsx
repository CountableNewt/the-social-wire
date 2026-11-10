import { afterEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Gateway from "@/lib/socialWireGatewayClient";
import { BlueskyAnalytics } from "@/components/Analytics/BlueskyAnalytics";
import { BlueskyPostTable } from "@/components/Analytics/BlueskyPostTable";
import { getBlueskyAnalytics, type BlueskyAnalyticsSummary } from "@/lib/analytics/blueskyAnalyticsClient";

export function blueskyAnalyticsFixture(): BlueskyAnalyticsSummary {
  return {
    sampledAt: "2026-11-09T12:00:00Z", days: 7,
    profile: { followers: 1200, following: 25, posts: 100 },
    activity: { posts: 14, replies: 3, postsPerDay: 2 },
    topPosts: [], shares: [],
    coverage: { feedComplete: true, feedAvailable: true, sharesComplete: false, sharesAvailable: true, articlesSearched: 0, feedPostsExamined: 14 },
  };
}

afterEach(cleanup);

describe("Bluesky analytics client", () => {
  it("uses the authenticated endpoint and forwards period and cancellation", async () => {
    const fetch = spyOn(Gateway, "gatewayFetch").mockResolvedValue(Response.json(blueskyAnalyticsFixture()));
    try {
      const session = { did: "did:plc:alice" } as unknown as OAuthSession;
      const signal = new AbortController().signal;
      expect((await getBlueskyAnalytics(session, { days: 7, signal })).days).toBe(7);
      expect(fetch).toHaveBeenCalledWith(session, "/v1/analytics/bluesky?days=7", { signal });
    } finally { fetch.mockRestore(); }
  });

  it("keeps private upstream error details out of displayed failures", async () => {
    const fetch = spyOn(Gateway, "gatewayFetch").mockResolvedValue(new Response("private provider details", { status: 503 }));
    try {
      await expect(getBlueskyAnalytics({} as OAuthSession, { days: 30 })).rejects.toThrow("Bluesky analytics could not be loaded (503)");
      fetch.mockResolvedValue(new Response("private auth details", { status: 401 }));
      await expect(getBlueskyAnalytics({} as OAuthSession, { days: 30 })).rejects.toThrow("Log in again");
      fetch.mockResolvedValue(new Response("private entitlement details", { status: 403 }));
      await expect(getBlueskyAnalytics({} as OAuthSession, { days: 30 })).rejects.toThrow("requires Pro access");
    } finally { fetch.mockRestore(); }
  });
});

describe("Bluesky analytics report", () => {
  it("distinguishes unavailable metrics from real zeroes and discloses current-count semantics", () => {
    const data = blueskyAnalyticsFixture();
    data.profile = { followers: 0, following: null, posts: 0 };
    data.activity = null;
    data.coverage.feedAvailable = false;
    render(<BlueskyAnalytics data={data} isPending={false} error={null} retry={() => {}} />);
    expect(screen.getAllByText("0")).toHaveLength(2);
    expect(screen.queryByText("—")).toBeNull();
    expect(screen.queryByText("Following")).toBeNull();
    expect(screen.queryByText("Posts in Period")).toBeNull();
    expect(screen.queryByText(/Posts per Day/)).toBeNull();
    expect(screen.getByText("Bluesky Posts Are Temporarily Unavailable.")).toBeTruthy();
    expect(screen.getByText(/Current Engagement Counts Can Include Activity Outside This Period/)).toBeTruthy();
    expect(screen.queryByText(/Follower Growth/)).toBeNull();
  });

  it("labels partial feed coverage and distinguishes unpublished inventory from no discovered shares", () => {
    const data = blueskyAnalyticsFixture();
    data.coverage.feedComplete = false;
    const view = render(<BlueskyAnalytics data={data} isPending={false} error={null} retry={() => {}} />);
    expect(screen.getByText(/Latest Posts Only; Period May Be Incomplete/)).toBeTruthy();
    expect(screen.getByText(/Refresh to Collect Articles With Public URLs/)).toBeTruthy();
    view.rerender(<BlueskyAnalytics data={{ ...data, coverage: { ...data.coverage, articlesSearched: 3 } }} isPending={false} error={null} retry={() => {}} />);
    expect(screen.getByText(/3 Article URLs Searched/)).toBeTruthy();
    expect(screen.getByText("No Article Shares Discovered in This Period.")).toBeTruthy();
    expect(screen.queryByText(/Refresh to Collect Articles With Public URLs/)).toBeNull();
  });

  it("renders top posts and article shares with internal post navigation and safe article links", () => {
    const data = blueskyAnalyticsFixture();
    const post = { uri: "at://did:plc:alice/app.bsky.feed.post/one", title: "Top Post", url: "javascript:alert(1)", publishedAt: "2026-11-08T12:00:00Z", engagement: { likes: 5, replies: 2, reposts: 1, quotes: 0 } };
    data.topPosts = [post];
    data.shares = [
      { articleUri: "at://did:plc:alice/site.standard.document/article", articleTitle: "My Article", articleUrl: "https://example.com/article", posts: [{ ...post, uri: "at://did:plc:bob/app.bsky.feed.post/two", title: "Shared Article" }] },
      { articleUri: "at://did:plc:alice/site.standard.document/unsafe", articleTitle: "Unsafe Article", articleUrl: "javascript:alert(1)", posts: [] },
    ];
    data.coverage.articlesSearched = 2;
    render(<BlueskyAnalytics data={data} isPending={false} error={null} retry={() => {}} />);
    expect(screen.getByRole("link", { name: "Top Post" }).getAttribute("href")).toContain("/social/post?");
    expect(screen.getByRole("link", { name: "Shared Article" }).getAttribute("href")).toContain("did%3Aplc%3Abob");
    expect(screen.getByRole("link", { name: "My Article" }).getAttribute("href")).toBe("https://example.com/article");
    expect(screen.queryByRole("link", { name: "Unsafe Article" })).toBeNull();
    expect(within(screen.getByRole("region", { name: "Top Bluesky Posts" })).getAllByRole("columnheader")).toHaveLength(5);
    expect(screen.getByText(/Search May Miss Shares/)).toBeTruthy();
  });

  it("shows loading and retryable errors without fabricating an empty report", () => {
    const retry = mock(() => {});
    const view = render(<BlueskyAnalytics isPending={true} error={null} retry={retry} />);
    expect(screen.getByRole("status").textContent).toBe("Loading Bluesky Analytics…");
    expect(screen.queryByText("No Posts Found in This Period.")).toBeNull();
    view.rerender(<BlueskyAnalytics isPending={false} error={new Error("Provider Unavailable")} retry={retry} />);
    expect(screen.getByRole("alert").textContent).toContain("Provider Unavailable");
    fireEvent.click(screen.getByRole("button", { name: "Retry Bluesky Analytics" }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it("preserves available share results when part of the provider search fails", () => {
    const data = blueskyAnalyticsFixture();
    data.coverage = { ...data.coverage, sharesAvailable: false, articlesSearched: 1 };
    data.shares = [{ articleUri: "at://did:plc:alice/site.standard.document/one", articleTitle: "Available Article", articleUrl: "https://example.com/article", posts: [] }];
    render(<BlueskyAnalytics data={data} isPending={false} error={null} retry={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("Some Article Share Searches Are Temporarily Unavailable");
    expect(screen.getByRole("link", { name: "Available Article" })).toBeTruthy();
  });

  it("keeps unavailable post counters and dates distinct from zero and rejects invalid post identities", () => {
    render(<BlueskyPostTable posts={[{ uri: "javascript:alert(1)", title: "Invalid Post", url: "https://example.com/invalid", publishedAt: "not-a-date", engagement: null }]} />);
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByText("Date Unavailable")).toBeTruthy();
    expect(screen.getAllByRole("columnheader")).toHaveLength(1);
    expect(screen.queryByText("—")).toBeNull();
  });

  it("keeps known zero counters and hides columns with no observations", () => {
    const post = { uri: "at://did:plc:alice/app.bsky.feed.post/one", title: "Observed Post", url: "", publishedAt: "2026-11-09T12:00:00Z", engagement: { likes: 0, replies: null, reposts: null, quotes: null } };
    render(<BlueskyPostTable posts={[post, { ...post, uri: "at://did:plc:alice/app.bsky.feed.post/two", title: "Unknown Post", engagement: null }]} />);
    expect(screen.getAllByRole("columnheader").map(header => header.textContent)).toEqual(["Post", "Likes"]);
    expect(screen.getByText("0")).toBeTruthy();
    expect(screen.getByText("—")).toBeTruthy();
  });

  it("omits metric cards when every count is unavailable", () => {
    const data = blueskyAnalyticsFixture();
    data.profile = { followers: null, following: null, posts: null }; data.activity = null;
    render(<BlueskyAnalytics data={data} isPending={false} error={null} retry={() => {}} />);
    expect(screen.queryByRole("definition")).toBeNull();
    expect(screen.queryByText("Followers")).toBeNull();
    expect(screen.queryByText("—")).toBeNull();
  });
});
