import { describe, expect, it } from "bun:test";

import type { MergedLatrSave } from "@/lib/pdsClient";
import {
  countSavedLinkCategories,
  filterSavedLinksByCategory,
  parseSavedLinkCategory,
  savedLinkCategory,
} from "@/lib/savedLinkCategory";

function native(subjectUri: string): MergedLatrSave {
  return {
    kind: "native",
    savedAt: "2026-01-01T00:00:00.000Z",
    itemRkey: "item",
    itemUri: "at://did:plc:viewer/link.latr.saved.item/item",
    subjectUri,
  };
}

function external(
  url: string,
  preview: { title?: string; excerpt?: string; linkedWebUrl?: string } = {},
): MergedLatrSave {
  return {
    kind: "external",
    normalizedUrl: url,
    url,
    savedAt: "2026-01-01T00:00:00.000Z",
    externalRkey: "ext",
    itemRkey: "item",
    externalUri: "at://did:plc:viewer/link.latr.saved.external/ext",
    itemUri: "at://did:plc:viewer/link.latr.saved.item/item",
    subjectUri: "at://did:plc:viewer/link.latr.saved.external/ext",
    ...preview,
  };
}

describe("savedLinkCategory", () => {
  it("classifies native standard.site documents as articles and Bluesky posts as social", () => {
    expect(savedLinkCategory(native("at://did:plc:author/site.standard.document/a"))).toBe("articles");
    expect(savedLinkCategory(native("at://did:plc:author/app.bsky.feed.post/p"))).toBe("social");
  });

  it("classifies social post URLs as social but not profile pages", () => {
    expect(savedLinkCategory(external("https://bsky.app/profile/alice.test/post/3k"))).toBe("social");
    expect(savedLinkCategory(external("https://x.com/alice/status/123"))).toBe("social");
    expect(savedLinkCategory(external("https://www.reddit.com/r/swift/comments/abc/title"))).toBe("social");
    expect(savedLinkCategory(external("https://bsky.app/profile/alice.test"))).toBe("other");
  });

  it("uses the linked web URL for wrapped social saves", () => {
    expect(
      savedLinkCategory(
        external("https://getpocket.com/read/1", {
          linkedWebUrl: "https://bsky.app/profile/alice.test/post/3k",
        }),
      ),
    ).toBe("social");
  });

  it("treats external links with a title and excerpt as articles and the rest as other", () => {
    expect(
      savedLinkCategory(external("https://example.com/story", { title: "Story", excerpt: "Summary" })),
    ).toBe("articles");
    expect(savedLinkCategory(external("https://example.com/story", { title: "Story" }))).toBe("other");
  });

  it("counts and filters rows by category", () => {
    const rows = [
      native("at://did:plc:author/site.standard.document/a"),
      native("at://did:plc:author/app.bsky.feed.post/p"),
      external("https://example.com/file.pdf"),
    ];
    expect(countSavedLinkCategories(rows)).toEqual({ articles: 1, social: 1, other: 1 });
    expect(filterSavedLinksByCategory(rows, "social")).toEqual([rows[1]!]);
    expect(filterSavedLinksByCategory(rows, null)).toEqual(rows);
  });

  it("parses only known category values", () => {
    expect(parseSavedLinkCategory("articles")).toBe("articles");
    expect(parseSavedLinkCategory("video")).toBeNull();
    expect(parseSavedLinkCategory(null)).toBeNull();
  });
});
