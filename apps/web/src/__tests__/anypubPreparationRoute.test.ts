import { expect, spyOn, test } from "bun:test";
import { POST } from "@/app/xrpc/at.anypub.prepare/route";

const endpoint = "https://testing.thesocialwire.app/xrpc/at.anypub.prepare";
const date = "2099-01-01T12:00:00.000Z";
const document = {
  site: "at://did:plc:author/site.standard.publication/publication",
  title: "Scheduled Article",
  publishedAt: date,
  path: "/example",
};

function request(body: string, contentType = "application/json") {
  return POST(new Request(endpoint, { method: "POST", headers: { "Content-Type": contentType }, body }));
}

async function prepare(input: Record<string, unknown>) {
  return request(JSON.stringify(input));
}

test("prepares a document locally with the existing record envelope and clear-field behavior", async () => {
  const fetcher = spyOn(globalThis, "fetch").mockRejectedValue(new Error("Unexpected Upstream Request"));
  try {
    const response = await prepare({ operation: "document", input: document, previous: { description: "old", extra: true }, clearFields: ["description"] });
    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toBe("no-store");
    expect(await response.json()).toEqual({ record: { ...document, $type: "site.standard.document", extra: true } });
    expect(fetcher).not.toHaveBeenCalled();
  } finally {
    fetcher.mockRestore();
  }
});

test("rejects malformed, unsupported, and oversized preparation requests", async () => {
  expect((await request("{}", "text/plain")).status).toBe(415);
  expect((await request("{broken")).status).toBe(400);
  expect((await request("[]")).status).toBe(400);
  expect((await prepare({ operation: "offload", host: "unknown", content: {} })).status).toBe(400);
  expect((await prepare({ operation: "article", native: { host: "unknown", markdown: "A", assets: [{ id: "image" }] } })).status).toBe(400);
  expect((await prepare({ operation: "document", input: { ...document, publishedAt: "2099-02-30T12:00:00Z" } })).status).toBe(400);
  expect((await prepare({ operation: "document", input: { ...document, site: document.site.replace(/publication$/, "..") } })).status).toBe(400);
  expect((await prepare({ operation: "document", input: { ...document, title: "é".repeat(501) } })).status).toBe(400);
  expect((await request("x".repeat(8 * 1024 * 1024 + 1))).status).toBe(413);
});

test.each(["/a/%2e%2e/b", "/%2fexample.com", "/foo%20bar", "/abc%", "/query%3fsecret"])("rejects unsafe decoded article path %s", async path => {
  expect((await prepare({ operation: "document", input: { ...document, path } })).status).toBe(400);
});

test("keeps the Markdown offload cutoff at 100 KiB of UTF-8", async () => {
  const body = "é".repeat(50 * 1024);
  const content = { $type: "at.markpub.markdown", text: { markdown: body } };
  const inline = await prepare({ operation: "offload", host: "markpub", content });
  expect(await inline.json()).toEqual({ prepared: { content } });
  const offloaded = await prepare({ operation: "offload", host: "markpub", content: { ...content, text: { markdown: `${body}a` } } });
  expect(await offloaded.json()).toMatchObject({ prepared: { offload: { kind: "markpub-markdown", mimeType: "text/markdown", body: `${body}a` } } });
});
