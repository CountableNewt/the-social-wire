import { afterEach, beforeEach, expect, mock, spyOn, test } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { OAuthSession } from "@atproto/oauth-client-browser";
import * as Auth from "@/hooks/useAuth";
import * as Profiles from "@/hooks/useViewerProfile";
import * as Editing from "@/lib/profileEditing";
import { ProfileEditDialog } from "@/components/Account/ProfileEditDialog";

const restores: (() => void)[] = [];
const snapshot: Editing.EditableProfileSnapshot = { cid: "old-cid", record: { displayName: "PDS Name", description: "PDS Bio", avatar: { ref: "avatar" }, banner: { ref: "banner" }, pronouns: "they/them" } };
let oauth: OAuthSession;
let cache: QueryClient;
let load: ReturnType<typeof spyOn<typeof Editing, "loadEditableProfile">>;
let save: ReturnType<typeof spyOn<typeof Editing, "saveEditableProfile">>;
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; }
function auth() { return { session: { did: oauth.did }, getOAuthSession: () => oauth } as ReturnType<typeof Auth.useAuth>; }
function show() { return render(<QueryClientProvider client={cache}><ProfileEditDialog /></QueryClientProvider>); }
async function open() { fireEvent.click(screen.getByRole("button", { name: "Edit Profile" })); return await screen.findByRole("textbox", { name: "Display Name" }); }
beforeEach(() => {
  oauth = { did: "did:plc:editor" } as unknown as OAuthSession;
  cache = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  spyOn(Auth, "useAuth").mockImplementation(auth);
  spyOn(Profiles, "useViewerProfile").mockReturnValue({ data: { did: oauth.did, displayName: "Stale AppView Name", avatar: "https://example.com/avatar.jpg", banner: "https://example.com/banner.jpg" } } as ReturnType<typeof Profiles.useViewerProfile>);
  load = spyOn(Editing, "loadEditableProfile").mockResolvedValue(snapshot);
  save = spyOn(Editing, "saveEditableProfile").mockResolvedValue({ ...snapshot, cid: "new-cid" });
  for (const [object, name, value] of [
    ...Object.entries({ HTMLElement: window.HTMLElement, HTMLInputElement: window.HTMLInputElement, Element: window.Element, Node: window.Node, MutationObserver: window.MutationObserver, DOMRect: window.DOMRect, getComputedStyle: window.getComputedStyle.bind(window), requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(0), 0), cancelAnimationFrame: clearTimeout }).map(([key, value]) => [globalThis, key, value] as const),
    [URL, "createObjectURL", () => "blob:profile-preview"], [URL, "revokeObjectURL", () => {}],
  ] as const) {
    const previous = Object.getOwnPropertyDescriptor(object, name);
    Object.defineProperty(object, name, { configurable: true, value });
    restores.push(() => { if (previous) Object.defineProperty(object, name, previous); else Reflect.deleteProperty(object, name); });
  }
});
afterEach(() => { cleanup(); cache.clear(); mock.restore(); restores.splice(0).reverse().forEach(restore => restore()); });

test("loads the canonical profile only when opened and exposes the supported fields", async () => {
  const pending = deferred<Editing.EditableProfileSnapshot>(); load.mockReturnValue(pending.promise);
  show(); expect(load).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Edit Profile" }));
  expect(screen.getByRole("dialog", { name: "Edit Profile" })).toBeTruthy();
  expect(screen.getByText("Loading Profile…")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Save Profile" })).toBeNull();
  await act(async () => pending.resolve(snapshot));
  await waitFor(() => expect(screen.getByRole("dialog", { name: "Edit Profile" }).contains(document.activeElement)).toBe(true));
  expect((screen.getByRole("textbox", { name: "Display Name" }) as HTMLInputElement).value).toBe("PDS Name");
  expect((screen.getByRole("textbox", { name: "Bio" }) as HTMLTextAreaElement).value).toBe("PDS Bio");
  expect(screen.getByLabelText("Avatar").getAttribute("accept")).toBe("image/jpeg,image/png");
  expect(screen.getByLabelText("Banner")).toBeTruthy();
  expect(screen.queryByRole("textbox", { name: /Handle|Password/ })).toBeNull();
  expect(load.mock.calls[0]?.[0]).toBe(oauth);
});

test("shows load errors and retries without writing", async () => {
  load.mockRejectedValueOnce(new Error("Profile Load Failed")); show();
  fireEvent.click(screen.getByRole("button", { name: "Edit Profile" }));
  expect((await screen.findByRole("alert")).textContent).toBe("Profile Load Failed");
  fireEvent.click(screen.getByRole("button", { name: "Retry Profile" }));
  await screen.findByRole("textbox", { name: "Display Name" });
  expect(load).toHaveBeenCalledTimes(2); expect(save).not.toHaveBeenCalled();
});

test("saves edited text, preserves untouched images, and refreshes the viewer cache", async () => {
  show(); const name = await open();
  fireEvent.change(name, { target: { value: "New Name" } });
  fireEvent.change(screen.getByRole("textbox", { name: "Bio" }), { target: { value: "New Bio" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Profile" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit Profile" })).toBeNull());
  expect(save).toHaveBeenCalledTimes(1);
  const options = save.mock.calls[0]![0];
  expect(options.snapshot).toBe(snapshot); expect(options.expectedDid).toBe(oauth.did); expect(options.session).toBe(oauth);
  expect(options.displayName).toBe("New Name"); expect(options.description).toBe("New Bio");
  expect(options.avatar).toBeUndefined(); expect(options.banner).toBeUndefined();
  expect(cache.getQueryData(Profiles.VIEWER_PROFILE_QUERY_KEY(oauth.did))).toMatchObject({ did: oauth.did, displayName: "New Name", description: "New Bio" });
  expect(screen.getByText("Profile Saved.")).toBeTruthy();
});

test.each(["Avatar", "Banner"] as const)("replaces %s with a selected file and removes the other image explicitly", async label => {
  show(); await open(); const file = new File(["jpeg"], "replacement.jpg", { type: "image/jpeg" });
  fireEvent.change(screen.getByLabelText(label), { target: { files: [file] } });
  await waitFor(() => expect(screen.getByRole("img", { name: `${label} Preview` }).getAttribute("src")).toBe("blob:profile-preview"));
  const other = label === "Avatar" ? "Banner" : "Avatar";
  fireEvent.click(screen.getByRole("button", { name: `Remove ${other}` }));
  expect(screen.queryByRole("img", { name: `${other} Preview` })).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Save Profile" }));
  await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
  expect(save.mock.calls[0]![0][label === "Avatar" ? "avatar" : "banner"]).toBe(file);
  expect(save.mock.calls[0]![0][other === "Avatar" ? "avatar" : "banner"]).toBeNull();
});

test("Cancel discards edits and reopening reloads text and images", async () => {
  show(); const name = await open(); fireEvent.change(name, { target: { value: "Discard Me" } });
  fireEvent.click(screen.getByRole("button", { name: "Remove Avatar" }));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit Profile" })).toBeNull());
  expect(save).not.toHaveBeenCalled();
  expect((await open() as HTMLInputElement).value).toBe("PDS Name");
  expect(screen.getByRole("img", { name: "Avatar Preview" })).toBeTruthy();
  expect(load).toHaveBeenCalledTimes(2);
});

test("save errors keep the draft available for retry", async () => {
  save.mockRejectedValueOnce(new Error("Profile Changed Elsewhere")); show(); const name = await open();
  fireEvent.change(name, { target: { value: "Keep Draft" } }); fireEvent.click(screen.getByRole("button", { name: "Save Profile" }));
  expect((await screen.findByRole("alert")).textContent).toBe("Profile Changed Elsewhere");
  expect((screen.getByRole("textbox", { name: "Display Name" }) as HTMLInputElement).value).toBe("Keep Draft");
  fireEvent.click(screen.getByRole("button", { name: "Save Profile" }));
  await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
});

test("pending save disables edits and dismissal, then account handoff aborts stale completion", async () => {
  const pending = deferred<Editing.EditableProfileSnapshot>(); save.mockReturnValue(pending.promise);
  const view = show(); await open(); fireEvent.click(screen.getByRole("button", { name: "Save Profile" }));
  const cancel = screen.getByRole("button", { name: "Cancel" });
  expect(cancel.hasAttribute("disabled")).toBe(true);
  expect((screen.getByRole("textbox", { name: "Display Name" }) as HTMLInputElement).matches(":disabled")).toBe(true);
  expect(screen.getByLabelText("Avatar").hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("button", { name: "Saving…" }).hasAttribute("disabled")).toBe(true);
  fireEvent.click(cancel); fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  expect(screen.getByRole("dialog", { name: "Edit Profile" })).toBeTruthy();
  const options = save.mock.calls[0]![0]; const oldDid = oauth.did;
  oauth = { did: "did:plc:new-viewer" } as unknown as OAuthSession;
  view.rerender(<QueryClientProvider client={cache}><ProfileEditDialog /></QueryClientProvider>);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit Profile" })).toBeNull());
  expect(options.signal?.aborted).toBe(true); expect(() => options.assertCurrent?.()).toThrow();
  await act(async () => pending.resolve(snapshot));
  expect(cache.getQueryData(Profiles.VIEWER_PROFILE_QUERY_KEY(oldDid))).toBeUndefined();
  expect(screen.queryByText("Profile Saved.")).toBeNull();
});

test("account handoff aborts a pending read and reopening loads the new account", async () => {
  const pending = deferred<Editing.EditableProfileSnapshot>(); load.mockReturnValueOnce(pending.promise);
  const view = show(); fireEvent.click(screen.getByRole("button", { name: "Edit Profile" }));
  const signal = load.mock.calls[0]![1]; oauth = { did: "did:plc:new-viewer" } as unknown as OAuthSession;
  view.rerender(<QueryClientProvider client={cache}><ProfileEditDialog /></QueryClientProvider>);
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit Profile" })).toBeNull());
  expect(signal?.aborted).toBe(true); await act(async () => pending.resolve(snapshot));
  await open(); expect(load.mock.calls[1]?.[0]).toBe(oauth);
});

test("requires the current OAuth session before reading or saving", async () => {
  spyOn(Auth, "useAuth").mockReturnValue({ session: { did: oauth.did }, getOAuthSession: () => null } as ReturnType<typeof Auth.useAuth>);
  show(); fireEvent.click(screen.getByRole("button", { name: "Edit Profile" }));
  expect((await screen.findByRole("alert")).textContent).toBe("Please Log In Again to Edit Your Profile.");
  expect(load).not.toHaveBeenCalled(); expect(save).not.toHaveBeenCalled();
});
