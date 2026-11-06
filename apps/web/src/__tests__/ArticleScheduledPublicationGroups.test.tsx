import { afterEach, beforeEach, expect, it, mock, spyOn } from "bun:test";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render as renderView, screen, waitFor, within } from "@testing-library/react";
import type { useSkejScheduling } from "@/hooks/useSkejScheduling";
import type { ArticlePublication } from "@/lib/articles/articlePublishingTypes";
import type { SkejArticleScheduleSummary } from "@/lib/skej/skejSchedulingClient";
import { ArticleScheduledList } from "@/components/Articles/ArticleScheduledList";
import * as Thumbnail from "@/components/Articles/ArticleLibraryThumbnail";

const did = "did:plc:writer";
const publications: ArticlePublication[] = ["First", "Second"].map(name => ({ uri: `at://${did}/site.standard.publication/${name.toLowerCase()}`, cid: name, name: `${name} Journal`, url: `https://${name.toLowerCase()}.test`, host: "unknown", record: {} }));
const render = (ui: React.ReactNode) => renderView(<QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>);
const restores: (() => void)[] = [];
const cancel = mock(async () => {}), reschedule = mock(async () => {}), onCanceled = mock(async () => {});
function schedule(id: string, site = publications[0].uri): SkejArticleScheduleSummary {
  return { did, rkey: id, scheduleUri: `at://${did}/at.skej.schedule/${id}`, scheduledAt: "2099-01-01T12:00:00Z", status: "scheduled", attempts: 0, record: { $type: "at.skej.schedule", status: "scheduled", createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-02T00:00:00Z", scheduledAt: "2099-01-01T12:00:00Z", timezonePolicy: "absolute_utc", publishRkey: id, recordType: "site.standard.document", posts: [], retry: { attemptCount: 0, maxAttempts: 8 }, shadowRecord: { $type: "site.standard.document", title: `Article ${id}`, site, publishedAt: "2099-01-01T12:00:00Z", coverImage: { $type: "blob", ref: { $link: "cover-cid" }, mimeType: "image/jpeg", size: 10 } } } };
}
function scheduler(items: SkejArticleScheduleSummary[]) {
  return { readiness: { data: { connected: true }, isPending: false, isError: false, refetch: async () => {} }, schedules: { data: items, isPending: false, isFetching: false, isError: false, refetch: async () => {} }, connectUrl: "https://skej.test", pending: false, cancel, reschedule } as unknown as ReturnType<typeof useSkejScheduling>;
}
beforeEach(() => {
  cancel.mockClear(); reschedule.mockClear(); onCanceled.mockClear();
  for (const [name, value] of Object.entries({ getComputedStyle: window.getComputedStyle.bind(window), requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0), cancelAnimationFrame: clearTimeout })) {
    const original = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { configurable: true, value });
    restores.push(() => { if (original) Object.defineProperty(globalThis, name, original); else Reflect.deleteProperty(globalThis, name); });
  }
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });
it("groups scheduled articles by publication and passes future document identity to thumbnails", () => {
  const thumb = spyOn(Thumbnail, "ArticleLibraryThumbnail").mockImplementation(() => <span data-testid="article-thumbnail" />);
  restores.push(() => thumb.mockRestore());
  const first = schedule("one"), second = schedule("two", publications[1].uri), unknown = schedule("three", `at://${did}/site.standard.publication/unknown`);
  render(<ArticleScheduledList did={did} scheduler={scheduler([first, second, unknown])} isPro onCanceled={onCanceled} publications={publications} />);
  expect(within(screen.getByRole("region", { name: "First Journal" })).getByText("Article one")).toBeTruthy();
  expect(within(screen.getByRole("region", { name: "Second Journal" })).getByText("Article two")).toBeTruthy();
  expect(within(screen.getByRole("region", { name: "Other Publications" })).getByText("Article three")).toBeTruthy();
  expect(document.body.textContent).not.toContain("at://");
  expect(thumb.mock.calls[0][0]).toMatchObject({ did, article: { uri: `at://${did}/site.standard.document/one`, cid: first.record.updatedAt, record: first.record.shadowRecord } });
});
it("preserves rescheduling and cancellation confirmation in grouped rows", async () => {
  const thumb = spyOn(Thumbnail, "ArticleLibraryThumbnail").mockImplementation(() => <span />); restores.push(() => thumb.mockRestore());
  const item = schedule("one");
  render(<ArticleScheduledList did={did} scheduler={scheduler([item])} isPro onCanceled={onCanceled} publications={publications} />);
  fireEvent.click(screen.getByRole("button", { name: "Reschedule" }));
  fireEvent.change(screen.getByLabelText("Publish At"), { target: { value: "2099-01-02T12:00" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Time" }));
  await waitFor(() => expect(reschedule).toHaveBeenCalledWith(item, new Date("2099-01-02T12:00").toISOString()));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  fireEvent.click(screen.getByRole("button", { name: "Cancel Schedule" }));
  expect(cancel).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Confirm Cancellation" }));
  await waitFor(() => expect(cancel).toHaveBeenCalledWith(item));
  expect(onCanceled).toHaveBeenCalledWith(item);
});
