import { expect, it } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { SocialExternalEmbed } from "@/components/Social/SocialExternalEmbed";

const external = { uri: "https://example.com/article", title: "Article", description: "Description", thumb: "https://example.com/cover.png" };

it("reserves external thumbnail space before image loading while preserving the complete image", () => {
  const node = document.createElement("div");
  node.innerHTML = renderToStaticMarkup(<SocialExternalEmbed external={external} href={external.uri} />);
  const image = node.querySelector("img")!;
  expect(image.parentElement!.className).toBe("relative h-48 w-full bg-muted sm:h-60");
  expect(image.className).toBe("absolute inset-0 h-full w-full object-contain");
  expect(image.getAttribute("src")).toBe(external.thumb);
  expect(node.querySelector("a")!.getAttribute("href")).toBe(external.uri);
});

it("does not reserve thumbnail space when an external preview has no safe image", () => {
  for (const thumb of [undefined, "javascript:alert(1)"]) {
    const html = renderToStaticMarkup(<SocialExternalEmbed external={{ ...external, thumb }} href={external.uri} />);
    expect(html).not.toContain("<img");
    expect(html).not.toContain("h-48");
  }
});
