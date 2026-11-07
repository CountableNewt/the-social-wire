import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Auth from "@/hooks/useAuth";
import * as Verification from "@/lib/socialVerification";
import * as Suggestions from "@/hooks/useLoginHandleSuggestions";
import { SocialVerificationProvider } from "@/contexts/SocialVerificationContext";
import { TrustedVerifiersSettingsSection } from "@/components/Account/TrustedVerifiersSettingsSection";
import { ProfileDisplayName } from "@/components/shared/ProfileDisplayName";
const viewer = "did:plc:abcdefghijklmnopqrstuvwx", other = "did:plc:bcdefghijklmnopqrstuvwxy", issuer = "did:plc:cdefghijklmnopqrstuvwxyz";
let did: string, client: QueryClient;
const restores: (() => void)[] = [];
const subject = { did: other, handle: "person.test", displayName: "Person", verification: { verifiedStatus: "valid", trustedVerifierStatus: "none", verifications: [] } };
beforeEach(() => {
  window.localStorage.clear(); did = viewer;
  const pointer = Object.getOwnPropertyDescriptor(globalThis, "PointerEvent");
  Object.defineProperty(globalThis, "PointerEvent", { configurable: true, value: window.PointerEvent ?? window.MouseEvent });
  restores.push(() => { if (pointer) Object.defineProperty(globalThis, "PointerEvent", pointer); else Reflect.deleteProperty(globalThis, "PointerEvent"); });
  for (const [name, value] of Object.entries({ requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0), cancelAnimationFrame: clearTimeout })) {
    const original = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { configurable: true, value });
    restores.push(() => { if (original) Object.defineProperty(globalThis, name, original); else Reflect.deleteProperty(globalThis, name); });
  }
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const suggestions = spyOn(Suggestions, "useLoginHandleSuggestions").mockReturnValue({ data: [], isFetching: false } as unknown as ReturnType<typeof Suggestions.useLoginHandleSuggestions>); restores.push(() => suggestions.mockRestore());
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: { did } } as ReturnType<typeof Auth.useAuth>)); restores.push(() => auth.mockRestore());
  const profile = spyOn(Verification, "resolveVerifierProfile").mockResolvedValue({ did: issuer, handle: "verifier.test" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); restores.push(() => profile.mockRestore());
  const records = spyOn(Verification, "loadCustomVerifications").mockResolvedValue([{ issuer, subject: other, handle: "person.test", displayName: "Person", uri: `at://${issuer}/app.bsky.graph.verification/one` }]); restores.push(() => records.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); window.localStorage.clear(); });
function App() { return <QueryClientProvider client={client}><SocialVerificationProvider><TrustedVerifiersSettingsSection /><ProfileDisplayName name="Person" subject={subject} /></SocialVerificationProvider></QueryClientProvider>; }
it.each(["keyboard", "pointer"])("selects a verifier suggestion with %s before explicitly adding it", async method => {
  const suggestions = spyOn(Suggestions, "useLoginHandleSuggestions").mockReturnValue({
    data: [{ did: issuer, handle: "verifier.test", displayName: "Example Verifier" }], isFetching: false,
  } as unknown as ReturnType<typeof Suggestions.useLoginHandleSuggestions>); restores.push(() => suggestions.mockRestore());
  render(<App />);
  const input = screen.getByRole("combobox", { name: "Verifier Handle or DID" });
  fireEvent.change(input, { target: { value: "ver" } });
  fireEvent.focus(input);
  if (method === "keyboard") {
    fireEvent.keyDown(input, { key: "ArrowDown" });
    fireEvent.keyDown(input, { key: "Enter" });
  } else {
    fireEvent.click(screen.getByRole("option", { name: /Example Verifier/ }));
  }
  expect((input as HTMLInputElement).value).toBe("verifier.test");
  expect(screen.queryByRole("listbox")).toBeNull();
  expect(Verification.resolveVerifierProfile).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Add Verifier" }));
  await waitFor(() => expect(Verification.resolveVerifierProfile).toHaveBeenCalledWith("verifier.test"));
  expect(await screen.findByRole("button", { name: "Remove Verifier: @verifier.test" })).toBeTruthy();
  expect((input as HTMLInputElement).value).toBe("");
});
it("accepts a manual DID when no suggestions are available", async () => {
  render(<App />);
  fireEvent.change(screen.getByRole("combobox", { name: "Verifier Handle or DID" }), { target: { value: issuer } });
  fireEvent.click(screen.getByRole("button", { name: "Add Verifier" }));
  await waitFor(() => expect(Verification.resolveVerifierProfile).toHaveBeenCalledWith(issuer));
  expect(await screen.findByRole("button", { name: "Remove Verifier: @verifier.test" })).toBeTruthy();
});
it("toggles official badges and adds/removes a custom verifier with immediate badge updates", async () => {
  render(<App />);
  expect(screen.getByRole("img", { name: "Verified on Bluesky" })).toBeTruthy();
  fireEvent.click(screen.getByRole("switch", { name: "Bluesky Verification" }));
  expect(screen.queryByRole("img")).toBeNull();
  fireEvent.change(screen.getByLabelText("Verifier Handle or DID"), { target: { value: "@verifier.test" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Verifier" }));
  expect(await screen.findByRole("img", { name: "Verified by a Custom Trusted Verifier" })).toBeTruthy();
  expect(JSON.parse(window.localStorage.getItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer)!)).toEqual({ includesBlueskyVerification: false, trustedVerifierDIDs: [issuer] });
  fireEvent.click(screen.getByRole("button", { name: "Remove Verifier: @verifier.test" }));
  expect(await screen.findByRole("dialog")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Remove Verifier" }));
  await waitFor(() => expect(screen.queryByRole("img")).toBeNull());
  expect(screen.getByText("No Custom Verifiers.")).toBeTruthy();
});
it("isolates settings by account and rejects a late lookup after account handoff", async () => {
  let finish!: (value: Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>) => void;
  const profile = spyOn(Verification, "resolveVerifierProfile").mockImplementation(() => new Promise(resolve => { finish = resolve; })); restores.push(() => profile.mockRestore());
  const view = render(<App />);
  fireEvent.click(screen.getByRole("switch", { name: "Bluesky Verification" }));
  fireEvent.change(screen.getByLabelText("Verifier Handle or DID"), { target: { value: "verifier.test" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Verifier" }));
  did = other; view.rerender(<App />);
  expect(screen.getByRole("img", { name: "Verified on Bluesky" })).toBeTruthy();
  finish({ did: issuer, handle: "verifier.test" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>);
  await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("Account Changed"));
  expect(window.localStorage.getItem(Verification.VERIFICATION_STORAGE_PREFIX + other)).toBeNull();
  expect(JSON.parse(window.localStorage.getItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer)!).trustedVerifierDIDs).toEqual([]);
});
it("keeps concurrent preference changes when the verifier lookup finishes", async () => {
  let finish!: (value: Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>) => void;
  const profile = spyOn(Verification, "resolveVerifierProfile").mockImplementation(() => new Promise(resolve => { finish = resolve; })); restores.push(() => profile.mockRestore());
  render(<App />);
  fireEvent.change(screen.getByLabelText("Verifier Handle or DID"), { target: { value: "verifier.test" } });
  fireEvent.click(screen.getByRole("button", { name: "Add Verifier" }));
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: false, trustedVerifierDIDs: [] }));
  finish({ did: issuer, handle: "verifier.test" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>);
  expect(await screen.findByRole("img", { name: "Verified by a Custom Trusted Verifier" })).toBeTruthy();
  expect(JSON.parse(window.localStorage.getItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer)!).includesBlueskyVerification).toBe(false);
});
