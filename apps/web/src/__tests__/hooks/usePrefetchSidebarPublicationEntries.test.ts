import { describe, expect, it } from "bun:test";

import { orderPublicationIdsForPrefetch } from "@/hooks/usePrefetchSidebarPublicationEntries";

describe("orderPublicationIdsForPrefetch", () => {
  it("dedupes and normalizes publication ids", () => {
    const ordered = orderPublicationIdsForPrefetch(
      [
        "at://did:plc:alice/site.standard.publication/a",
        "at://did:plc:alice/site.standard.publication/a",
        "at://did:plc:bob/site.standard.publication/b",
      ],
      null
    );

    expect(ordered).toEqual([
      "at://did:plc:alice/site.standard.publication/a",
      "at://did:plc:bob/site.standard.publication/b",
    ]);
  });

  it("puts the selected publication first", () => {
    const ordered = orderPublicationIdsForPrefetch(
      [
        "at://did:plc:alice/site.standard.publication/a",
        "at://did:plc:bob/site.standard.publication/b",
        "at://did:plc:carol/site.standard.publication/c",
      ],
      "at://did:plc:carol/site.standard.publication/c"
    );

    expect(ordered[0]).toBe("at://did:plc:carol/site.standard.publication/c");
    expect(ordered).toHaveLength(3);
  });

  it("ignores selected ids that are not in the sidebar list", () => {
    const ordered = orderPublicationIdsForPrefetch(
      ["at://did:plc:alice/site.standard.publication/a"],
      "at://did:plc:missing/site.standard.publication/x"
    );

    expect(ordered).toEqual(["at://did:plc:alice/site.standard.publication/a"]);
  });
});
