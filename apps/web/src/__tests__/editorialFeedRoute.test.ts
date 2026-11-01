import { algorithmicFeedsEnabled } from "@/lib/algorithmicFeeds";
import { beforeEach, afterEach, describe, expect, it } from "bun:test";

import { editorialFeedForReadRoute } from "@/lib/editorialFeedRoute";

const originalAlgorithmicFlag = process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
beforeEach(() => { process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "true"; });
afterEach(() => {
  if (originalAlgorithmicFlag === undefined) delete process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED;
  else process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = originalAlgorithmicFlag;
});

describe("editorialFeedForReadRoute", () => {
  it("recognizes The Wire and Your Circle as editorial feeds", () => {
    expect(editorialFeedForReadRoute("/read", "wire")).toBe("wire");
    expect(editorialFeedForReadRoute("/read", "circle")).toBe("circle");
    expect(editorialFeedForReadRoute("/read", "finance")).toBe("finance");
  });

  it("keeps publication feeds on the read-state shell", () => {
    expect(editorialFeedForReadRoute("/read", "subscribed")).toBeNull();
    expect(editorialFeedForReadRoute("/read", "following")).toBeNull();
    expect(editorialFeedForReadRoute("/read/publication", "circle")).toBeNull();
  });
});

it("does not classify suspended queries as editorial feeds", () => {
  process.env.NEXT_PUBLIC_ALGORITHMIC_FEEDS_ENABLED = "false";
  expect(algorithmicFeedsEnabled()).toBe(false);
  for (const feed of ["wire", "circle", "finance", "sports"]) expect(editorialFeedForReadRoute("/read", feed)).toBeNull();
});
