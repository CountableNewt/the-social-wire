import { afterEach, describe, expect, it, mock, spyOn } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import { renderToStaticMarkup } from "react-dom/server";
import { ArticleContentLabels } from "@/components/shared/ArticleContentLabels";
import { EntryDetail } from "@/components/EntryDetail/EntryDetail";
import * as Entries from "@/hooks/useEntries";
import * as Embed from "@/components/EntryDetail/EntryArticleEmbed";
import { articleContentLabelFields, documentContentLabels, normalizeArticleContentLabels } from "@/lib/articleContentLabels";
import { getEntry, parseEntryValue } from "@/lib/atprotoClient";
import { clearLockedEntryArticlePresentationsForTests } from "@/lib/entryArticlePresentation";

const record = { $type: "site.standard.document", title: "Publisher Article", content: "<p>Visible article text.</p>", labels: { $type: "com.atproto.label.defs#selfLabels", values: [{ val: "sexual" }, { val: "ai-generated" }, { val: "sexual" }, { val: "unknown" }] } };
afterEach(() => { cleanup(); mock.restore(); clearLockedEntryArticlePresentationsForTests(); });

describe("publisher document content labels", () => {
  it("recognizes only exact known labels, with deterministic order and no duplicates", () => {
    expect(normalizeArticleContentLabels(["sexual", "ai-generated", "sexual", "AI-generated", "porn", "nsfw", "ai"])).toEqual(["ai-generated", "sexual"]);
    for (const invalid of [undefined, null, "sexual", {}, ["AI", "nsfw"], [{ val: "sexual" }]]) expect(articleContentLabelFields(invalid)).toEqual({});
    expect(documentContentLabels(record)).toEqual(["ai-generated", "sexual"]);
    expect(parseEntryValue(record).contentLabels).toEqual(["ai-generated", "sexual"]);
    for (const invalid of [
      { ...record, $type: "blog.pckt.document" }, { ...record, $type: "site.standard.publication" },
      { ...record, labels: { values: [{ val: "sexual" }] } },
      { ...record, labels: { $type: "com.atproto.label.defs#selfLabels", values: ["sexual", { val: "NSFW" }] } },
      { title: "Legacy", contentLabels: ["sexual"], nsfw: true, aiGenerated: true },
    ]) expect(parseEntryValue(invalid)).not.toHaveProperty("contentLabels");
  });
  it("renders only static AI and NSFW badges with publisher attribution", () => {
    const { container } = render(<ArticleContentLabels contentLabels={["sexual", "ai-generated", "unknown", "sexual"]} />);
    expect(screen.getByText("AI").title).toBe("Made With AI · Labeled by the Publisher");
    expect(screen.getByText("NSFW").title).toContain("Labeled by the Publisher");
    expect(container.textContent).toBe("AINSFW");
    expect(container.querySelector("button,a,[role],[tabindex]")).toBeNull();
    expect(renderToStaticMarkup(<ArticleContentLabels />)).toBe("");
    expect(renderToStaticMarkup(<ArticleContentLabels contentLabels={["unknown"]} />)).toBe("");
  });
  it("preserves canonical document self-labels in legacy detail reads", async () => {
    const fetchImpl = Object.assign(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input instanceof Request ? input.url : input.toString();
      if (url.includes("plc.directory")) return Response.json({ service: [{ id: "#atproto_pds", type: "AtprotoPersonalDataServer", serviceEndpoint: "https://pds.content-labels.example" }] });
      if (url.includes("com.atproto.repo.getRecord")) return Response.json({ uri: "at://did:plc:contentlabelslegacy/site.standard.document/one", cid: "cid", value: record });
      throw new Error(`Unexpected request: ${url}`);
    }, { preconnect: globalThis.fetch.preconnect });
    const fetcher = spyOn(globalThis, "fetch").mockImplementation(fetchImpl);
    const entry = await getEntry("at://did:plc:contentlabelslegacy/site.standard.document/one");
    expect(entry?.contentLabels).toEqual(["ai-generated", "sexual"]);
    expect(entry?.contentHtml).toBe("<p>Visible article text.</p>");
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

for (const contentLabels of [undefined, ["ai-generated", "sexual"]]) {
  for (const embed of [false, true]) it(`keeps ${embed ? "iframe" : "HTML"} content visible with ${contentLabels ? "publisher labels" : "no labels"}`, () => {
    const entry = { entryId: `at://did:plc:publisher/site.standard.document/${embed}-${!!contentLabels}`, title: "Visible Article", contentHtml: "<p>Visible article text.</p>", publishedAt: "2026-11-09", contentLabels, ...(embed ? { embedUrl: "https://publisher.example/article" } : {}) };
    spyOn(Entries, "useEntry").mockReturnValue({ data: entry, isLoading: false } as ReturnType<typeof Entries.useEntry>);
    spyOn(Embed, "EntryArticleEmbed").mockImplementation(({ url, title }) => <iframe src={url} title={title} />);
    const { container } = render(<EntryDetail entryId={entry.entryId} />);
    if (embed) expect(screen.getByTitle("Visible Article").getAttribute("src")).toBe(entry.embedUrl!);
    else expect(screen.getByText("Visible article text.")).toBeTruthy();
    expect(container.querySelector("article")?.getAttribute("hidden")).toBeNull();
    expect(screen.queryByText("AI") === null).toBe(!contentLabels);
    expect(screen.queryByText("NSFW") === null).toBe(!contentLabels);
    if (contentLabels && embed) expect(screen.getByText("NSFW").closest("iframe")).toBeNull();
    expect(screen.queryByRole("button", { name: /Show|Reveal|Continue/ })).toBeNull();
  });
}
