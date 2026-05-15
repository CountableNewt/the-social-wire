import { describe, it, expect } from "bun:test";
import {
  rssItemsSortedNewestFirst,
  rssParserItemToListItem,
} from "@/lib/rssFeedServer";

const MIN_RSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>Hello</title>
      <link>https://example.com/hello</link>
      <pubDate>Mon, 01 Jan 2024 00:00:00 GMT</pubDate>
    </item>
  </channel>
</rss>`;

describe("rssFeedServer", () => {
  it("parses RSS and maps list rows", async () => {
    const norm = "https://example.com/feed.xml";
    const items = await rssItemsSortedNewestFirst(MIN_RSS, norm);
    expect(items.length).toBe(1);
    const row = rssParserItemToListItem(norm, items[0]!);
    expect(row.title).toBe("Hello");
    expect(row.entryId.startsWith("rssentry:")).toBe(true);
  });
});
