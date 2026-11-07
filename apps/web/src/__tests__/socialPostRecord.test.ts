import { describe, expect, it } from "bun:test";
import { AppBskyFeedPost } from "@atproto/api";
import { validateSocialPostRecord } from "@/lib/socialPostRecord";
const base = { $type: "app.bsky.feed.post" as const, text: "Ordinary post", createdAt: "2026-11-06T00:00:00Z" };
describe("upstream post record reading", () => {
  for (const text of ["x".repeat(301), "🦋".repeat(1100), "a".repeat(3500) + " Tail"]) it(`preserves all ${new TextEncoder().encode(text).length} UTF-8 bytes beyond authoring limits`, () => {
    const record = { ...base, text };
    expect(AppBskyFeedPost.validateRecord(record).success).toBe(false);
    const result = validateSocialPostRecord(record);
    expect(result.success).toBe(true);
    if (result.success) expect(result.value.text).toBe(text);
    expect(record.text).toBe(text);
    expect(AppBskyFeedPost.validateRecord(record).success).toBe(false);
  });
  it("preserves facets at the end of long Unicode text without changing source bytes", () => {
    const prefix = "🦋".repeat(400), text = prefix + "Tail Link";
    const facets = [{ index: { byteStart: new TextEncoder().encode(prefix).length, byteEnd: new TextEncoder().encode(text).length }, features: [{ $type: "app.bsky.richtext.facet#link", uri: "https://example.com/tail" }] }];
    const result = validateSocialPostRecord({ ...base, text, facets });
    expect(result.success).toBe(true);
    if (result.success) { expect(result.value.text).toBe(text); expect(result.value.facets).toEqual(facets); }
  });
  for (const patch of [{ text: 42 }, { text: undefined }, { createdAt: "invalid" }, { reply: { root: { uri: 42, cid: "bad" } } }, { facets: [{ index: { byteStart: -1, byteEnd: 1 }, features: [] }] }, { embed: { $type: "app.bsky.embed.images", images: [{ alt: 42 }] } }]) it(`rejects malformed ${Object.keys(patch)[0]}`, () => {
    expect(validateSocialPostRecord({ ...base, text: "x".repeat(400), ...patch }).success).toBe(false);
  });
  it("keeps ordinary posts unchanged", () => {
    const result = validateSocialPostRecord(base);
    expect(result.success).toBe(true);
    if (result.success) expect(result.value).toEqual(base);
  });
});
