import { describe, expect, it } from "bun:test";

import type { MergedLatrSave } from "@/lib/pdsClient";
import {
  isOriginalEntryContentUri,
  latrSaveFallbackEntryDetail,
  originalEntryIdFromLatrSave,
} from "@/lib/savedLinkSocialTarget";

describe("savedLinkSocialTarget", () => {
  it("recognizes original entry content collections", () => {
    expect(
      isOriginalEntryContentUri(
        "at://did:plc:author/site.standard.document/post123"
      )
    ).toBe(true);
    expect(
      isOriginalEntryContentUri("at://did:plc:viewer/com.latr.saved.item/item")
    ).toBe(false);
  });

  it("resolves native saves to the original subject entry AT-URI", () => {
    const row: MergedLatrSave = {
      kind: "native",
      savedAt: "2026-01-01T00:00:00.000Z",
      itemRkey: "item",
      itemUri: "at://did:plc:viewer/com.latr.saved.item/item",
      subjectUri: "at://did:plc:author/site.standard.document/post123",
    };
    expect(originalEntryIdFromLatrSave(row)).toBe(
      "at://did:plc:author/site.standard.document/post123"
    );
  });

  it("does not treat external wrapper subjects as original entries", () => {
    const row: MergedLatrSave = {
      kind: "external",
      normalizedUrl: "https://example.com/post",
      url: "https://example.com/post",
      savedAt: "2026-01-01T00:00:00.000Z",
      externalRkey: "ext",
      itemRkey: "item",
      externalUri: "at://did:plc:viewer/com.latr.saved.external/ext",
      itemUri: "at://did:plc:viewer/com.latr.saved.item/item",
      subjectUri: "at://did:plc:viewer/com.latr.saved.external/ext",
    };
    expect(originalEntryIdFromLatrSave(row)).toBeNull();
  });

  it("builds quote fallback entry detail from external saves without wrapper URIs", () => {
    const row: MergedLatrSave = {
      kind: "external",
      normalizedUrl: "https://example.com/post",
      url: "https://example.com/post",
      savedAt: "2026-01-01T00:00:00.000Z",
      externalRkey: "ext",
      itemRkey: "item",
      externalUri: "at://did:plc:viewer/com.latr.saved.external/ext",
      itemUri: "at://did:plc:viewer/com.latr.saved.item/item",
      subjectUri: "at://did:plc:viewer/com.latr.saved.external/ext",
      title: "Example Post",
    };
    const fallback = latrSaveFallbackEntryDetail(row);
    expect(fallback?.entryId).toBe("saved-link:external");
    expect(fallback?.originalUrl).toBe("https://example.com/post");
    expect(fallback?.title).toBe("Example Post");
  });
});
