"use client";

import { Analytics } from "@vercel/analytics/next";
import { SpeedInsights } from "@vercel/speed-insights/next";
import { vercelAnalyticsUrl } from "@/lib/vercelAnalyticsUrl";

const beforeSend = <T extends { url: string }>(event: T): T | null => {
  const url = vercelAnalyticsUrl(event.url);
  return url ? { ...event, url } : null;
};

export function VercelAnalytics({ speedInsights }: { speedInsights: boolean }) {
  return (
    <>
      <Analytics beforeSend={beforeSend} />
      {speedInsights && <SpeedInsights beforeSend={beforeSend} />}
    </>
  );
}
