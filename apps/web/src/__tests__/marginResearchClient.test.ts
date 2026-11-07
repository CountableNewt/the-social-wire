import { afterEach, expect, it, mock, spyOn } from "bun:test";
import type { Agent } from "@atproto/api";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Atproto from "@/lib/atprotoClient";
import { listMarginAnnotations, parseMarginAnnotation } from "@/lib/research/marginResearchClient";
const did = "did:plc:writer", uri = `at://${did}/at.margin.note/one`;
const session = { did } as unknown as OAuthSession;
const record = { $type: "at.margin.note", motivation: "highlighting", target: { source: "https://example.com", title: "Source", selector: [{ type: "TextPositionSelector" }, { refinedBy: { exact: "Quoted Passage" } }] }, body: { value: "A Note" }, tags: ["research", null], createdAt: "2026-11-01T12:00:00Z" };
let restore: (() => void) | undefined;
afterEach(() => { restore?.(); restore = undefined; });
function agent(list: ReturnType<typeof mock>) {
  const spy = spyOn(Atproto, "createOAuthAgent").mockReturnValue({ com: { atproto: { repo: { listRecords: list } } } } as unknown as Agent);
  restore = () => spy.mockRestore();
}
it("validates annotation ownership/type and maps nested exact quotes", () => {
  expect(parseMarginAnnotation(uri, record, did)).toMatchObject({ quote: "Quoted Passage", body: "A Note", tags: ["research"], title: "Source" });
  expect(parseMarginAnnotation("at://did:plc:other/at.margin.note/one", record, did)).toBeNull();
  expect(parseMarginAnnotation(uri, { ...record, $type: "wrong.type" }, did)).toBeNull();
  expect(parseMarginAnnotation(uri, { ...record, createdAt: "invalid" }, did)).toBeNull();
});
it("paginates, skips malformed records, deduplicates and sorts newest first", async () => {
  const list = mock().mockResolvedValueOnce({ data: { records: [{ uri, value: record }], cursor: "next" } }).mockResolvedValueOnce({ data: { records: [{ uri, value: record }, { uri: `at://${did}/at.margin.note/new`, value: { ...record, createdAt: "2026-11-02T12:00:00Z" } }, { uri: "invalid", value: record }] } });
  agent(list);
  const result = await listMarginAnnotations(session);
  expect(result).toHaveLength(2);
  expect(result[0].uri).toEndWith("/new");
  expect(list.mock.calls[1][0]).toMatchObject({ cursor: "next", limit: 100, collection: "at.margin.note", repo: did });
});
it("rejects repeated pagination cursors", async () => {
  const list = mock().mockResolvedValue({ data: { records: [], cursor: "repeat" } }); agent(list);
  await expect(listMarginAnnotations(session)).rejects.toThrow("repeated a Margin pagination cursor");
  expect(list).toHaveBeenCalledTimes(2);
});
it("bounds pagination and honors abort before and after a request", async () => {
  const list = mock().mockImplementation(async () => ({ data: { records: [], cursor: `page-${list.mock.calls.length}` } })); agent(list);
  await expect(listMarginAnnotations(session)).rejects.toThrow("too many pages");
  expect(list).toHaveBeenCalledTimes(100); list.mockClear();
  const aborted = new AbortController(); aborted.abort();
  await expect(listMarginAnnotations(session, aborted.signal)).rejects.toThrow(); expect(list).not.toHaveBeenCalled();
  const controller = new AbortController();
  list.mockImplementation(async () => { controller.abort(); return { data: { records: [], cursor: "next" } }; });
  await expect(listMarginAnnotations(session, controller.signal)).rejects.toThrow(); expect(list).toHaveBeenCalledTimes(1);
});
