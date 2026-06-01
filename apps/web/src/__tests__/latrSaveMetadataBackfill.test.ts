import { describe, expect, it } from "bun:test";

import {
  backfillUrlForLatrSave,
  isLatrSaveMetadataSparse,
  mergeLatrSaveBackfillMetadata,
} from "@/lib/latrSaveMetadataBackfill";
import type { MergedLatrSave } from "@/lib/pdsClient";

describe("latrSaveMetadataBackfill", () => {
  const externalRow: MergedLatrSave = {
    kind: "external",
    normalizedUrl: "https://example.com/article",
    url: "https://example.com/article",
    savedAt: "2026-06-01T12:00:00.000Z",
    externalRkey: "EXT",
    itemRkey: "ITEM",
    externalUri: "at://did/com.latr.saved.external/EXT",
    itemUri: "at://did/com.latr.saved.item/ITEM",
    subjectUri: "at://did/com.latr.saved.external/EXT",
  };

  it("detects sparse external rows missing image and title", () => {
    expect(isLatrSaveMetadataSparse(externalRow)).toBe(true);
  });

  it("detects hostname-only titles as sparse", () => {
    expect(
      isLatrSaveMetadataSparse({
        ...externalRow,
        title: "example.com",
        image: "https://example.com/thumb.jpg",
      })
    ).toBe(true);
  });

  it("treats enriched rows as complete", () => {
    expect(
      isLatrSaveMetadataSparse({
        ...externalRow,
        title: "Example Article",
        image: "https://example.com/thumb.jpg",
      })
    ).toBe(false);
  });

  it("resolves backfill URL for external and native rows", () => {
    expect(backfillUrlForLatrSave(externalRow)).toBe("https://example.com/article");

    const nativeRow: MergedLatrSave = {
      kind: "native",
      savedAt: "2026-06-01T12:00:00.000Z",
      itemRkey: "ITEM2",
      itemUri: "at://did/com.latr.saved.item/ITEM2",
      subjectUri: "at://did/app/site.standard.document/abc",
      linkedWebUrl: "https://news.example/story",
    };
    expect(backfillUrlForLatrSave(nativeRow)).toBe("https://news.example/story");
  });

  it("merges preview fields without clobbering existing metadata", () => {
    const merged = mergeLatrSaveBackfillMetadata(
      { ...externalRow, title: "Kept title" },
      {
        title: "Preview title",
        excerpt: "Preview excerpt",
        image: "https://cdn.example/thumb.jpg",
        site: "Example",
      }
    );

    expect(merged.title).toBe("Kept title");
    expect(merged.excerpt).toBe("Preview excerpt");
    expect(merged.image).toBe("https://cdn.example/thumb.jpg");
    expect(merged.site).toBe("Example");
  });
});
