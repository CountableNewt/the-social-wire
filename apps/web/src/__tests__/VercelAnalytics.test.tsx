import { afterEach, expect, spyOn, test } from "bun:test";
import { cleanup, render } from "@testing-library/react";
import * as WebSDK from "@vercel/analytics/next";
import * as SpeedSDK from "@vercel/speed-insights/next";
import type { BeforeSend } from "@vercel/analytics";
import type { SpeedInsightsProps } from "@vercel/speed-insights";
import { VercelAnalytics } from "@/components/shared/VercelAnalytics";

const restores: (() => void)[] = [];
afterEach(() => { cleanup(); restores.splice(0).forEach(restore => restore()); });

test("both analytics SDKs sanitize private navigation before transmission", () => {
  let web: BeforeSend | undefined;
  let speed: SpeedInsightsProps["beforeSend"];
  const analytics = spyOn(WebSDK, "Analytics").mockImplementation(props => { web = props.beforeSend; return null; });
  const insights = spyOn(SpeedSDK, "SpeedInsights").mockImplementation(props => { speed = props.beforeSend; return null; });
  restores.push(() => analytics.mockRestore(), () => insights.mockRestore());
  render(<VercelAnalytics speedInsights />);
  const url = "https://thesocialwire.app/read/at%3Aprivate?entry=unpublished#secret";
  expect(web?.({ type: "pageview", url })).toEqual({ type: "pageview", url: "https://thesocialwire.app/read/[publication]" });
  expect(speed?.({ type: "vital", url })).toEqual({ type: "vital", url: "https://thesocialwire.app/read/[publication]" });
  expect(web?.({ type: "pageview", url: "invalid" })).toBeNull();
});

test("development page views do not consume the Hobby Speed Insights project", () => {
  const analytics = spyOn(WebSDK, "Analytics").mockImplementation(() => null);
  const insights = spyOn(SpeedSDK, "SpeedInsights").mockImplementation(() => null);
  restores.push(() => analytics.mockRestore(), () => insights.mockRestore());
  render(<VercelAnalytics speedInsights={false} />);
  expect(analytics).toHaveBeenCalledTimes(1);
  expect(insights).not.toHaveBeenCalled();
});
