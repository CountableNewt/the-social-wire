import { expect, it } from "bun:test";
import { groupArticlesByPublication } from "@/lib/articles/articleLibraryPresentation";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";

const publication = (name: string, url: string): ArticlePublication => ({ uri: `at://did:plc:author/site.standard.publication/${name.toLowerCase()}`, cid: name, name, url, host: "unknown", record: {} });
const alpha = publication("Alpha Journal", "https://example.test/journal");
const zeta = publication("Zeta Journal", "https://zeta.test");

it("combines canonical publication URIs and normalized site URL aliases", () => {
  const rows = [{ id: "uri", site: alpha.uri }, { id: "url", site: "https://EXAMPLE.TEST:443/journal/" }, { id: "url-trim", site: " https://example.test/journal " }];
  expect(groupArticlesByPublication(rows, [alpha], row => row.site)).toEqual([{ id: alpha.uri, name: alpha.name, items: rows }]);
});

it("uses friendly separate labels for unknown and unassigned publications", () => {
  const unknown = { id: "unknown", site: "at://did:plc:unknown/site.standard.publication/one" };
  const unknownUrl = { id: "unknown-url", site: "https://unavailable.test" };
  const blank = { id: "blank", site: "  " };
  const missing = { id: "missing", site: undefined };
  const groups = groupArticlesByPublication([unknown, blank, unknownUrl, missing], [alpha], row => row.site);
  expect(groups).toEqual([
    { id: "unassigned", name: "No Publication Selected", items: [blank, missing] },
    { id: "unknown", name: "Other Publications", items: [unknown, unknownUrl] },
  ]);
  expect(groups.map(group => group.name).join(" ")).not.toContain("at://");
});

it("sorts named groups before fallbacks and retains article order within each publication", () => {
  const rows = [{ id: "unknown", site: "missing" }, { id: "zeta-new", site: zeta.uri }, { id: "alpha-new", site: alpha.uri }, { id: "zeta-old", site: zeta.url }, { id: "alpha-old", site: alpha.url }];
  const groups = groupArticlesByPublication(rows, [zeta, alpha], row => row.site);
  expect(groups.map(group => group.name)).toEqual([alpha.name, zeta.name, "Other Publications"]);
  expect(groups[0].items.map(item => item.id)).toEqual(["alpha-new", "alpha-old"]);
  expect(groups[1].items.map(item => item.id)).toEqual(["zeta-new", "zeta-old"]);
  expect(groupArticlesByPublication([], [alpha], row => row)).toEqual([]);
});
