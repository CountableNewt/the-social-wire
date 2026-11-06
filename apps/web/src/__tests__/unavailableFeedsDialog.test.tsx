import { afterEach, beforeAll, expect, it, mock } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { UnavailableFeedsDialog } from "@/components/FeedHeader/UnavailableFeedsDialog";
import type { DiscoveredPublication } from "@/lib/atprotoClient";
import { PublicationSourceRefreshError } from "@/lib/publicationSourceRefreshError";
import { rssPublicationIdFromNormalizedFeedUrl } from "@/lib/rssFeedCore";

const publication = (title: string, path: string): DiscoveredPublication => ({
  publicationId: rssPublicationIdFromNormalizedFeedUrl(`https://publisher.test/${path}`),
  title, authorDid: "did:web:skyreader.rss", authorHandle: "publisher.test", discoveredAt: "",
  subscriptionPublicationId: `at://did:plc:viewer/app.skyreader.feed.subscription/${path}`,
});
const broken = publication("World News", "world");
const healthy = publication("Technology News", "technology");
const warning = new PublicationSourceRefreshError(["publisher.test"], [broken.publicationId]);

beforeAll(() => {
  globalThis.getComputedStyle ??= window.getComputedStyle.bind(window);
  globalThis.requestAnimationFrame ??= callback => setTimeout(() => callback(performance.now()), 0) as unknown as number;
  globalThis.cancelAnimationFrame ??= id => clearTimeout(id);
});
afterEach(cleanup);

async function openFeeds() {
  fireEvent.click(screen.getByRole("button", { name: "View Unavailable Feeds" }));
  await screen.findByRole("dialog", { name: "Unavailable Feeds" });
}
async function openConfirmation() {
  await openFeeds();
  fireEvent.click(screen.getByRole("button", { name: "Unsubscribe From World News" }));
  await screen.findByRole("dialog", { name: "Unsubscribe?" });
}

it("names the exact unavailable feed and never offers its healthy same-host neighbor", async () => {
  const remove = mock(async () => {});
  render(<UnavailableFeedsDialog warning={warning} publications={[broken, healthy]} onUnsubscribe={remove} />);
  await openFeeds();
  expect(screen.getByText("World News")).toBeTruthy();
  expect(screen.getByText("https://publisher.test/world")).toBeTruthy();
  expect(screen.queryByText("Technology News")).toBeNull();
  expect(remove).not.toHaveBeenCalled();
});

it("requires confirmation and cancel does not unsubscribe", async () => {
  const remove = mock(async () => {});
  render(<UnavailableFeedsDialog warning={warning} publications={[broken]} onUnsubscribe={remove} />);
  await openConfirmation();
  expect(remove).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(remove).not.toHaveBeenCalled();
  await openFeeds();
  expect(screen.getByText("World News")).toBeTruthy();
});

it("confirms once, disables dismissal while pending, and removes only the unsubscribed feed", async () => {
  let finish!: () => void;
  const remove = mock(() => new Promise<void>(resolve => { finish = resolve; }));
  const another = publication("Another Feed", "another");
  render(<UnavailableFeedsDialog warning={new PublicationSourceRefreshError(["publisher.test"], [broken.publicationId, another.publicationId])} publications={[broken, healthy, another]} onUnsubscribe={remove} />);
  await openConfirmation();
  fireEvent.click(screen.getByRole("button", { name: "Unsubscribe" }));
  expect((screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Unsubscribing…" }));
  expect(remove).toHaveBeenCalledTimes(1);
  expect(remove).toHaveBeenCalledWith(broken);
  await act(async () => finish());
  await screen.findByRole("dialog", { name: "Unavailable Feeds" });
  expect(screen.queryByText("World News")).toBeNull();
  expect(screen.getByText("Another Feed")).toBeTruthy();
  expect(screen.queryByText("Technology News")).toBeNull();
});

it("keeps failed unsubscribe confirmation open and permits retry", async () => {
  const remove = mock(async () => {}).mockRejectedValueOnce(new Error("Could Not Remove Subscription"));
  render(<UnavailableFeedsDialog warning={warning} publications={[broken]} onUnsubscribe={remove} />);
  await openConfirmation();
  fireEvent.click(screen.getByRole("button", { name: "Unsubscribe" }));
  expect((await screen.findByRole("alert")).textContent).toBe("Could Not Remove Subscription");
  expect(screen.getByRole("dialog", { name: "Unsubscribe?" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Unsubscribe" }));
  await screen.findByText("You’ve unsubscribed from all feeds in this list.");
  expect(remove).toHaveBeenCalledTimes(2);
});

it("never guesses an unsubscribe target from a host or an unmatched source ID", async () => {
  const remove = mock(async () => {});
  const { rerender } = render(<UnavailableFeedsDialog warning={warning} publications={[healthy]} onUnsubscribe={remove} />);
  await openFeeds();
  expect(screen.getByText("https://publisher.test/world")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Unsubscribe/ })).toBeNull();
  rerender(<UnavailableFeedsDialog warning={new PublicationSourceRefreshError(["publisher.test"])} publications={[healthy]} onUnsubscribe={remove} />);
  expect(screen.getByText("publisher.test")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Unsubscribe/ })).toBeNull();
  expect(remove).not.toHaveBeenCalled();
});

it("does not offer unsubscribe when the current row has no subscription record", async () => {
  render(<UnavailableFeedsDialog warning={warning} publications={[{ ...broken, subscriptionPublicationId: undefined }]} onUnsubscribe={mock(async () => {})} />);
  await openFeeds();
  expect(screen.getByText("World News")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Unsubscribe/ })).toBeNull();
});

for (const change of ["removed", "replaced"] as const) {
  it(`does not unsubscribe a stale confirmation when its subscription is ${change}`, async () => {
    const remove = mock(async () => {});
    const { rerender } = render(<UnavailableFeedsDialog warning={warning} publications={[broken]} onUnsubscribe={remove} />);
    await openConfirmation();
    rerender(<UnavailableFeedsDialog warning={warning} publications={change === "removed" ? [] : [{ ...broken, subscriptionPublicationId: "at://did:plc:viewer/app.skyreader.feed.subscription/replacement" }]} onUnsubscribe={remove} />);
    fireEvent.click(screen.getByRole("button", { name: "Unsubscribe" }));
    expect((await screen.findByRole("alert")).textContent).toContain("This subscription has changed");
    expect(remove).not.toHaveBeenCalled();
  });
}
