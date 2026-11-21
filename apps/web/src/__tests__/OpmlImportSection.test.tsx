import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, waitFor, within } from "@testing-library/react";
import * as auth from "@/hooks/useAuth";
import * as publications from "@/hooks/usePublications";
import { OpmlImportSection } from "@/components/Account/OpmlImportSection";
import type { OpmlImportBatchResult } from "@/lib/opmlImport";

const domRestores: (() => void)[] = [];
beforeEach(() => {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "HTMLElement");
  Object.defineProperty(globalThis, "HTMLElement", { configurable: true, value: window.HTMLElement });
  domRestores.push(() => { if (previous) Object.defineProperty(globalThis, "HTMLElement", previous); else Reflect.deleteProperty(globalThis, "HTMLElement"); });
});

const restores: (() => void)[] = [];
afterEach(async () => { await act(async () => { cleanup(); await new Promise(resolve => setTimeout(resolve, 0)); }); restores.splice(0).reverse().forEach(restore => restore()); domRestores.splice(0).reverse().forEach(restore => restore()); });
const xml = '<opml><body><outline text="Example Blog" xmlUrl="https://example.org/feed.xml" /></body></opml>';
const success: OpmlImportBatchResult = { imported: [{ title: "Example Blog", feedUrl: "https://example.org/feed.xml", sourceIndex: 0, categoryPath: [] }], skippedExisting: [], failed: [] };
function setup() {
  const authSpy = spyOn(auth, "useAuth").mockReturnValue({ session: { did: "did:alice" } } as ReturnType<typeof auth.useAuth>);
  const publicationImport = mock(async (input: { feeds: unknown[] }) => { void input; return success; });
  const pub = spyOn(publications, "useImportOpmlFeedSubscriptions").mockReturnValue({ mutateAsync: publicationImport } as unknown as ReturnType<typeof publications.useImportOpmlFeedSubscriptions>);
  const subs = spyOn(publications, "useSkyreaderFeedSubscriptions").mockReturnValue({ data: [], isLoading: false, error: null } as unknown as ReturnType<typeof publications.useSkyreaderFeedSubscriptions>);
  restores.push(() => authSpy.mockRestore(), () => pub.mockRestore(), () => subs.mockRestore());
  return { authSpy, publicationImport };
}
async function upload(queries: ReturnType<typeof within>) {
  fireEvent.change(queries.getByLabelText("Choose OPML File"), { target: { files: [new File([xml], "feeds.opml")] } });
  await queries.findByLabelText(/Example Blog/);
}

it("imports OPML feeds as publications without a destination picker", async () => {
  const fixture = setup();
  const view = render(<OpmlImportSection />);
  const queries = within(view.container);
  expect(queries.queryByRole("combobox", { name: "Import To" })).toBeNull();
  expect(queries.queryByLabelText("Private Feeds")).toBeNull();
  await upload(queries);
  fireEvent.click(queries.getByRole("button", { name: "Import 1 Feed" }));
  await waitFor(() => expect(fixture.publicationImport).toHaveBeenCalledTimes(1));
  expect(fixture.publicationImport.mock.calls[0]?.[0]).toEqual(expect.objectContaining({ feeds: expect.arrayContaining([expect.objectContaining({ feedUrl: "https://example.org/feed.xml" })]) }));
});

it("resets the reviewed file on account change", async () => {
  const fixture = setup();
  const view = render(<OpmlImportSection />);
  const queries = within(view.container);
  await upload(queries);
  fixture.authSpy.mockReturnValue({ session: { did: "did:bob" } } as ReturnType<typeof auth.useAuth>);
  view.rerender(<OpmlImportSection />);
  expect(queries.queryByText("feeds.opml")).toBeNull();
  expect(queries.queryByLabelText(/Example Blog/)).toBeNull();
});
