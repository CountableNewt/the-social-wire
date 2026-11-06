import { afterEach, expect, it, spyOn } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import * as Embed from "@/components/EntryDetail/EntryArticleEmbed";
import { PublishedArticleView } from "@/components/Articles/PublishedArticleView";
import type { ArticlePublication, PublishedArticle } from "@/lib/articles/articlePublishingTypes";

afterEach(cleanup);
const publication: ArticlePublication = { uri: "at://did:plc:author/site.standard.publication/main", cid: "publication", name: "Example Publication", url: "https://example.test", host: "markpub", record: {} };
const article = (record: Record<string, unknown>): PublishedArticle => ({ uri: "at://did:plc:author/site.standard.document/story", cid: "article", record });
const props = { publications: [publication], metadataVisible: true, metadataWidth: 320, onMetadataWidthChange() {} };

it("renders markdown and publication metadata as a read-only article", () => {
  render(<PublishedArticleView {...props} article={article({ title: "Published Story", site: publication.uri, path: "/story", description: "Story Summary", publishedAt: "2026-10-10T12:00:00Z", tags: ["writing", 42, "research"], content: { $type: "at.markpub.markdown", text: { markdown: "## Published Section\n\nA published paragraph." } } })} />);
  expect(screen.getByRole("heading", { name: "Published Story" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "Published Section" })).toBeTruthy();
  expect(screen.getByText("A published paragraph.")).toBeTruthy();
  expect(screen.getByText("Story Summary")).toBeTruthy();
  expect(screen.getByText("Example Publication")).toBeTruthy();
  expect(screen.getByText("writing, research")).toBeTruthy();
  const link = screen.getByRole("link", { name: "Open Article" });
  expect(link.getAttribute("href")).toBe("https://example.test/story");
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(screen.queryByRole("button", { name: "Publish Article" })).toBeNull();
});

it("keeps native plaintext available when there is no safe article URL", () => {
  render(<PublishedArticleView {...props} metadataVisible={false} article={article({ title: "Native Article", url: "javascript:alert(1)", textContent: "Native article body.", content: { $type: "pub.leaflet.content" } })} />);
  expect(screen.getByText("Native article body.")).toBeTruthy();
  expect(screen.queryByRole("link", { name: "Open Article" })).toBeNull();
  expect(screen.queryByRole("complementary", { name: "Article Details" })).toBeNull();
});

it("passes a canonical native article URL and plaintext fallback to the shared embed", () => {
  const embed = spyOn(Embed, "EntryArticleEmbed").mockImplementation(({ fallbackContent, title }) => <div aria-label={title}>{fallbackContent}</div>);
  try {
    const value = article({ title: "Native Story", site: publication.uri, path: "/native", textContent: "Fallback body.", content: { $type: "pub.leaflet.content" } });
    render(<PublishedArticleView {...props} article={value} />);
    expect(screen.getByText("Fallback body.")).toBeTruthy();
    expect(embed.mock.calls[0]?.[0]).toMatchObject({ url: "https://example.test/native", title: "Native Story", expectedAtUri: value.uri });
  } finally { embed.mockRestore(); }
});
