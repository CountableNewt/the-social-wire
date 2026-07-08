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

  it("prioritizes selected and top unread publications without enqueueing the full sidebar", () => {
    const ids = Array.from({ length: 12 }, (_, index) => `pub-${index + 1}`);
    const unreadCounts = new Map([
      ["pub-6", 10],
      ["pub-3", 4],
      ["pub-9", 7],
    ]);

    const ordered = orderPublicationIdsForPrefetch(ids, "pub-4", unreadCounts);

    expect(ordered).toEqual([
      "pub-4",
      "pub-6",
      "pub-9",
      "pub-3",
      "pub-1",
      "pub-2",
      "pub-5",
      "pub-7",
    ]);
  });
});
