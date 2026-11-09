import type { OAuthSession } from "@atproto/oauth-client-browser";
import { gatewayFetch } from "@/lib/socialWireGatewayClient";
import type { EngagementMetrics } from "./creatorAnalyticsClient";

export type BlueskyAnalyticsPost = {
  uri: string;
  title: string;
  url: string;
  publishedAt: string;
  engagement: EngagementMetrics | null;
};

export type BlueskyAnalyticsSummary = {
  sampledAt: string;
  days: number;
  profile: { followers: number | null; following: number | null; posts: number | null };
  activity: { posts: number; replies: number; postsPerDay: number } | null;
  topPosts: BlueskyAnalyticsPost[];
  shares: { articleUri: string; articleTitle: string; articleUrl: string; posts: BlueskyAnalyticsPost[] }[];
  coverage: { feedComplete: boolean; feedAvailable: boolean; sharesComplete: false; sharesAvailable: boolean; articlesSearched: number; feedPostsExamined: number };
};

export async function getBlueskyAnalytics(session: OAuthSession, options: { days: number; signal?: AbortSignal }): Promise<BlueskyAnalyticsSummary> {
  const response = await gatewayFetch(session, `/v1/analytics/bluesky?${new URLSearchParams({ days: String(options.days) })}`, { signal: options.signal });
  if (!response.ok) {
    throw new Error(response.status === 401 ? "Log in again to allow Analytics to access your account." : response.status === 403 ? "Bluesky analytics requires Pro access for this account." : `Bluesky analytics could not be loaded (${response.status}). Please try again.`);
  }
  return await response.json() as BlueskyAnalyticsSummary;
}
