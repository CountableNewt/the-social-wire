import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import type { ModerationOpts } from "@atproto/api";
import * as Images from "@/hooks/useCachedImageUrl";
import { SocialNotificationGroupHeader } from "@/components/Social/SocialNotificationGroupHeader";
import type { SocialNotification } from "@/lib/blueskySocialDiscoveryClient";
import { groupSocialNotifications } from "@/lib/socialNotificationGrouping";
import { socialProfilePageHref } from "@/lib/socialProfileNavigation";

const labelerDid = "did:plc:labeler";
const moderation: ModerationOpts = {
  userDid: "did:plc:viewer",
  prefs: { adultContentEnabled: false, labels: {}, labelers: [{ did: labelerDid, labels: {} }], mutedWords: [], hiddenPosts: [] },
};
const bob = { did: "did:plc:bob", handle: "bob.test", displayName: "Bob", avatar: "https://images.test/bob.jpg" };
const carol = { did: "did:plc:carol", handle: "carol.test", displayName: "Carol", avatar: "https://images.test/carol.jpg" };
const dan = { did: "did:plc:dan", handle: "dan.test", displayName: "Dan" };
const restores: (() => void)[] = [];

function notification(id: string, author: SocialNotification["author"], isRead = true): SocialNotification {
  return { uri: `at://${author.did}/app.bsky.graph.follow/${id}`, cid: "bafyreia", author, reason: "follow", record: {}, isRead, indexedAt: "2026-10-11T12:00:00Z" };
}

beforeEach(() => {
  const images = spyOn(Images, "useCachedImageUrl").mockImplementation(src => ({ objectUrl: src ?? undefined, failed: false }));
  restores.push(() => images.mockRestore());
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });

it("discloses each unique person with their internal profile link", () => {
  const group = groupSocialNotifications([
    notification("one", bob), notification("two", carol), notification("three", bob), notification("four", dan),
  ])[0];
  render(<SocialNotificationGroupHeader group={group} moderation={moderation} />);
  const summary = screen.getByLabelText("Show People: Bob and Carol + 1");
  expect(summary.parentElement?.hasAttribute("open")).toBe(false);
  fireEvent.click(summary);
  expect(summary.parentElement?.hasAttribute("open")).toBe(true);
  expect(screen.getAllByRole("link")).toHaveLength(8);
  for (const author of [bob, carol, dan]) {
    expect(screen.getByRole("link", { name: author.displayName }).getAttribute("href")).toBe(socialProfilePageHref(author.did, "/social/notifications"));
  }
  expect(screen.getByText("Followed You")).toBeTruthy();
});

it("withholds moderated avatars and display names in both summary and disclosed people", () => {
  const hidden = { ...bob, labels: [{ src: labelerDid, uri: bob.did, val: "!hide", cts: "2026-10-11T00:00:00Z" }] };
  const group = groupSocialNotifications([notification("one", hidden), notification("two", carol)])[0];
  const { container } = render(<SocialNotificationGroupHeader group={group} moderation={moderation} />);
  fireEvent.click(screen.getByLabelText("Show People: bob.test and Carol"));
  expect(screen.queryByText("Bob")).toBeNull();
  expect(screen.getByRole("link", { name: "bob.test" }).getAttribute("href")).toBe(socialProfilePageHref(bob.did, "/social/notifications"));
  expect(container.querySelector(`img[src="${bob.avatar}"]`)).toBeNull();
  expect(container.querySelectorAll(`img[src="${carol.avatar}"]`)).toHaveLength(2);
});

it("shows unread for any unread member and removes it after all events are read", () => {
  const events = [notification("one", bob), notification("two", carol, false)];
  const { rerender } = render(<SocialNotificationGroupHeader group={groupSocialNotifications(events)[0]} moderation={moderation} />);
  expect(screen.getByText("Unread")).toBeTruthy();
  rerender(<SocialNotificationGroupHeader group={groupSocialNotifications(events.map(event => ({ ...event, isRead: true })))[0]} moderation={moderation} />);
  expect(screen.queryByText("Unread")).toBeNull();
});

it("links a single person directly without a disclosure", () => {
  render(<SocialNotificationGroupHeader group={groupSocialNotifications([notification("one", bob)])[0]} moderation={moderation} />);
  expect(screen.getByRole("link", { name: "Bob" }).getAttribute("href")).toBe(socialProfilePageHref(bob.did, "/social/notifications"));
  expect(screen.queryByLabelText(/Show People/)).toBeNull();
});


it("shows a single actor's trimmed pronouns beside their linked name", () => {
  const author = { ...bob, pronouns: "  he/him  " };
  render(<SocialNotificationGroupHeader group={groupSocialNotifications([notification("one", author)])[0]} moderation={moderation} />);
  const link = screen.getByRole("link", { name: "Bob he/him" });
  expect(within(link).getByText("he/him")).toBeTruthy();
  expect(link.getAttribute("href")).toBe(socialProfilePageHref(bob.did, "/social/notifications"));
  expect(screen.queryByLabelText(/Show People/)).toBeNull();
});

it("shows each person's pronouns in the grouped summary and disclosed list", () => {
  const authors = [{ ...bob, pronouns: "he/him" }, { ...carol, pronouns: "she/her" }, { ...dan, pronouns: "they/them" }];
  const group = groupSocialNotifications(authors.map((author, index) => notification(String(index), author)))[0];
  render(<SocialNotificationGroupHeader group={group} moderation={moderation} />);
  const summary = screen.getByLabelText("Show People: Bob he/him and Carol she/her + 1");
  expect(within(summary).getByText("he/him")).toBeTruthy();
  expect(within(summary).getByText("she/her")).toBeTruthy();
  expect(within(summary).queryByText("they/them")).toBeNull();
  fireEvent.click(summary);
  const people = screen.getByRole("list", { name: "People" });
  for (const author of authors) {
    const link = within(people).getByRole("link", { name: `${author.displayName} ${author.pronouns}` });
    expect(link.getAttribute("href")).toBe(socialProfilePageHref(author.did, "/social/notifications"));
  }
});

it.each([undefined, "", "   "])("omits absent or blank notification pronouns: %p", pronouns => {
  render(<SocialNotificationGroupHeader group={groupSocialNotifications([notification("one", { ...bob, pronouns })])[0]} moderation={moderation} />);
  expect(screen.getByRole("link", { name: "Bob" }).textContent).toBe("Bob");
});

it("withholds pronouns with moderated identity in every notification name surface", () => {
  const hidden = { ...bob, pronouns: "private-pronouns", labels: [{ src: labelerDid, uri: bob.did, val: "!hide", cts: "2026-10-11T00:00:00Z" }] };
  const view = render(<SocialNotificationGroupHeader group={groupSocialNotifications([notification("one", hidden)])[0]} moderation={moderation} />);
  expect(screen.queryByText("private-pronouns")).toBeNull();
  expect(screen.getByRole("link", { name: "bob.test" })).toBeTruthy();
  view.rerender(<SocialNotificationGroupHeader group={groupSocialNotifications([notification("one", hidden), notification("two", carol)])[0]} moderation={moderation} />);
  const summary = screen.getByLabelText("Show People: bob.test and Carol");
  expect(summary.textContent).toBe("bob.test and Carol");
  fireEvent.click(summary);
  expect(screen.queryByText("private-pronouns")).toBeNull();
  expect(within(screen.getByRole("list", { name: "People" })).getByRole("link", { name: "bob.test" })).toBeTruthy();
});
