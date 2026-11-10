import { afterAll, afterEach, beforeAll, beforeEach, expect, it, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import * as Auth from "@/hooks/useAuth";
import * as Social from "@/hooks/useEntrySocial";
import * as Saved from "@/hooks/useLatrSaved";
import * as Recommendations from "@/hooks/useStandardSiteRecommendation";
import * as Feedback from "@/hooks/useWireArticleFeedback";
import * as DraftDialog from "@/components/Articles/SendArticleToDraftDialog";
import { ArticleSocialToolbar } from "@/components/EntryDetail/ArticleSocialToolbar";
import type { EntryDetail } from "@/lib/atprotoClient";

const descriptors = [globalThis, window].flatMap(target => ["requestAnimationFrame", "cancelAnimationFrame"].map(name => ({ target, name, descriptor: Object.getOwnPropertyDescriptor(target, name) })));
const originalStyle = Object.getOwnPropertyDescriptor(globalThis, "getComputedStyle");
beforeAll(() => {
  Object.defineProperty(globalThis, "getComputedStyle", { configurable: true, value: window.getComputedStyle.bind(window) });
  for (const target of [globalThis, window]) {
    Object.defineProperty(target, "requestAnimationFrame", { configurable: true, value: (callback: FrameRequestCallback) => window.setTimeout(() => callback(performance.now()), 0) });
    Object.defineProperty(target, "cancelAnimationFrame", { configurable: true, value: (id: number) => window.clearTimeout(id) });
  }
});
afterAll(() => {
  if (originalStyle) Object.defineProperty(globalThis, "getComputedStyle", originalStyle); else Reflect.deleteProperty(globalThis, "getComputedStyle");
  for (const { target, name, descriptor } of descriptors) { if (descriptor) Object.defineProperty(target, name, descriptor); else Reflect.deleteProperty(target, name); }
});
let signedIn = true;
const restores: (() => void)[] = [];
beforeEach(() => {
  signedIn = true;
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: signedIn ? { did: "did:plc:viewer" } : null, getOAuthSession: () => null } as ReturnType<typeof Auth.useAuth>));
  const mutation = { isPending: false, mutate() {}, reset() {} };
  const social = spyOn(Social, "useEntrySocial").mockReturnValue({ viewerQuery: { data: {}, isLoading: false }, toggleLikeMutation: mutation, toggleRepostMutation: mutation, postMutation: mutation, replyMutation: mutation, quoteMutation: mutation, hasLinkedPost: false } as unknown as ReturnType<typeof Social.useEntrySocial>);
  const saved = spyOn(Saved, "useEntryIsLatrSaved").mockReturnValue(false);
  const saving = spyOn(Saved, "useSaveReadLaterEntryMutation").mockReturnValue(mutation as unknown as ReturnType<typeof Saved.useSaveReadLaterEntryMutation>);
  const recommendations = spyOn(Recommendations, "useStandardSiteRecommendation").mockReturnValue({ applicable: false } as ReturnType<typeof Recommendations.useStandardSiteRecommendation>);
  const feedback = spyOn(Feedback, "useWireArticleFeedback").mockReturnValue({ applicable: false } as ReturnType<typeof Feedback.useWireArticleFeedback>);
  const dialog = spyOn(DraftDialog, "SendArticleToDraftDialog").mockImplementation(({ url }) => <div role="dialog">Draft Source: {url}</div>);
  restores.push(...[auth, social, saved, saving, recommendations, feedback, dialog].map(spy => () => spy.mockRestore()));
});
afterEach(() => { cleanup(); restores.splice(0).reverse().forEach(restore => restore()); });
const entry: EntryDetail = { entryId: "at://did:plc:author/site.standard.document/article", title: "Source Article", publishedAt: "2026-11-10T00:00:00Z", contentHtml: "", originalUrl: "https://publisher.test/article" };
it("opens Send to Draft from the signed-in article menu with the article link", async () => {
  render(<ArticleSocialToolbar entry={entry} variant="menu" />);
  fireEvent.click(screen.getByRole("button", { name: "Article Actions" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Send to Draft" }));
  expect((await screen.findByRole("dialog")).textContent).toContain("https://publisher.test/article");
});
it.each(["signed-out", "missing-url"])("omits Send to Draft when %s", async state => {
  signedIn = state !== "signed-out";
  render(<ArticleSocialToolbar entry={state === "missing-url" ? { ...entry, originalUrl: undefined } : entry} variant="menu" />);
  fireEvent.click(screen.getByRole("button", { name: "Article Actions" }));
  await screen.findByRole("menu");
  expect(screen.queryByRole("menuitem", { name: "Send to Draft" })).toBeNull();
});
