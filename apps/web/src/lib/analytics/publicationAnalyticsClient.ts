import type { OAuthSession } from "@atproto/oauth-client-browser";
import { gatewayFetch } from "@/lib/socialWireGatewayClient";

export type PublicationAnalyticsMetrics = {
  uri: string;
  name: string;
  url: string;
  documents: number;
  subscribers: number | null;
  recommendations: number | null;
  recommendationCoverageComplete: boolean;
};

export type PublicationAnalyticsSummary = {
  sampledAt: string;
  publications: PublicationAnalyticsMetrics[];
  inventoryComplete: boolean;
};

export async function getPublicationAnalytics(
  session: OAuthSession,
  { signal }: { signal?: AbortSignal } = {},
): Promise<PublicationAnalyticsSummary> {
  const response = await gatewayFetch(session, "/v1/analytics/publications", { signal });
  if (response.status === 401) throw new Error("Log in again to allow Analytics to access your account.");
  if (response.status === 403) throw new Error("Publication analytics requires Pro access for this account.");
  if (!response.ok) throw new Error(`Publication analytics could not be loaded (${response.status}). Please try again.`);
  return await response.json() as PublicationAnalyticsSummary;
}
