import { afterEach, describe, expect, it } from "bun:test";
import { algorithmicFeedsEnabled, availableReaderFeed } from "@/lib/algorithmicFeeds";

const original = process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
afterEach(() => {
  if (original === undefined) delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
  else process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = original;
});

describe("algorithmic feed suspension", () => {
  it("defaults off and redirects saved and explicit algorithmic destinations to subscriptions", () => {
    delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
    expect(algorithmicFeedsEnabled()).toBe(false);
    for (const feed of ["wire", "circle", "finance", "sports"]) expect(availableReaderFeed(feed)).toBe("subscribed");
    for (const feed of [null, "following", "subscribed"]) expect(availableReaderFeed(feed)).toBe(feed);
  });
  it("allows a deliberate reactivation without discarding saved destinations", () => {
    process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "true";
    for (const feed of ["wire", "circle", "finance", "sports"]) expect(availableReaderFeed(feed)).toBe(feed);
  });
});
