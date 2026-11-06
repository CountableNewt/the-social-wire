import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { SocialExternalEmbed } from "@/components/Social/SocialExternalEmbed";
import { StandardSiteArticleCard } from "@/components/Social/StandardSiteArticleCard";

const external = { uri: "https://example.com/article", title: "Article", description: "Description", thumb: "https://example.com/cover.png" };

it("reserves the same thumbnail space before load and after standard.site enrichment", () => {
  const fallback = renderToStaticMarkup(<SocialExternalEmbed external={external} href={external.uri} />);
  const enriched = renderToStaticMarkup(<StandardSiteArticleCard thumb={external.thumb} metadata={{
    documentUri: "at://did:plc:author/site.standard.document/article", publicationUri: "at://did:plc:author/site.standard.publication/site",
    publicationName: "Publication", publicationUrl: "https://example.com", articleUrl: external.uri,
    title: external.title, description: external.description, authorDid: "did:plc:author",
  }} />);
  for (const html of [fallback, enriched]) {
    const node = document.createElement("div");
    node.innerHTML = html;
    const image = node.querySelector("img")!;
    expect(image.parentElement!.className).toBe("relative h-48 w-full bg-muted sm:h-60");
    expect(image.className).toBe("absolute inset-0 h-full w-full object-contain");
    expect(image.getAttribute("src")).toBe(external.thumb);
  }
});

it("does not reserve thumbnail space when an external preview has no safe image", () => {
  for (const thumb of [undefined, "javascript:alert(1)"]) {
    const html = renderToStaticMarkup(<SocialExternalEmbed external={{ ...external, thumb }} href={external.uri} />);
    expect(html).not.toContain("<img");
    expect(html).not.toContain("h-48");
  }
});
