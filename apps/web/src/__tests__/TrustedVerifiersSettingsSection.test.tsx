import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Auth from "@/hooks/useAuth";
import * as Verification from "@/lib/socialVerification";
import * as CachedImage from "@/hooks/useCachedImageUrl";
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
  const auth = spyOn(Auth, "useAuth").mockImplementation(() => ({ session: { did } } as ReturnType<typeof Auth.useAuth>)); restores.push(() => auth.mockRestore());
  const profile = spyOn(Verification, "resolveVerifierProfile").mockResolvedValue({ did: issuer, handle: "verifier.test" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); restores.push(() => profile.mockRestore());
  const records = spyOn(Verification, "loadCustomVerifications").mockResolvedValue([{ issuer, subject: other, handle: "person.test", displayName: "Person", uri: `at://${issuer}/app.bsky.graph.verification/one` }]); restores.push(() => records.mockRestore());
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); window.localStorage.clear(); });
function App() { return <QueryClientProvider client={client}><SocialVerificationProvider><TrustedVerifiersSettingsSection /><ProfileDisplayName name="Person" subject={subject} /></SocialVerificationProvider></QueryClientProvider>; }
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

it("hydrates saved verifier identities with avatar, display name and handle on mount", async () => {
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] }));
  const profile = spyOn(Verification, "resolveVerifierProfile").mockResolvedValue({ did: issuer, handle: "verifier.test", displayName: "Example Verifier", avatar: "https://images.example/verifier.png" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); restores.push(() => profile.mockRestore());
  const images = spyOn(CachedImage, "useCachedImageUrl").mockImplementation(src => ({ objectUrl: src ?? undefined, failed: false })); restores.push(() => images.mockRestore());
  render(<App />);
  expect(await screen.findByText("Example Verifier")).toBeTruthy();
  expect(screen.getByText("@verifier.test")).toBeTruthy();
  expect(screen.getByRole("img", { name: "Example Verifier" }).getAttribute("src")).toBe("https://images.example/verifier.png");
  expect(within(screen.getByRole("list")).queryByText(issuer)).toBeNull();
  expect(profile).toHaveBeenCalledWith(issuer, expect.any(AbortSignal));
  expect(screen.getByRole("button", { name: "Remove Verifier: @verifier.test" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "Remove Verifier: @verifier.test" }));
  expect(await screen.findByRole("dialog")).toBeTruthy();
  expect(JSON.parse(window.localStorage.getItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer)!).trustedVerifierDIDs).toEqual([issuer]);
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(screen.getByText("Example Verifier")).toBeTruthy();
});

it("retains a removable DID fallback when saved profile metadata cannot be loaded", async () => {
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] }));
  const profile = spyOn(Verification, "resolveVerifierProfile").mockRejectedValue(new Error("Unavailable")); restores.push(() => profile.mockRestore());
  render(<App />);
  await waitFor(() => expect(client.getQueryState(["verificationIssuerProfile", issuer])?.status).toBe("error"));
  expect(within(screen.getByRole("list")).getByText(issuer)).toBeTruthy();
  expect(screen.getByRole("button", { name: `Remove Verifier: ${issuer}` })).toBeTruthy();
});

it("never labels a saved DID with another account's returned profile", async () => {
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] }));
  const profile = spyOn(Verification, "resolveVerifierProfile").mockResolvedValue({ did: other, handle: "wrong.test", displayName: "Wrong Identity" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); restores.push(() => profile.mockRestore());
  render(<App />);
  await waitFor(() => expect(client.getQueryState(["verificationIssuerProfile", issuer])?.status).toBe("error"));
  expect(screen.queryByText("Wrong Identity")).toBeNull();
  expect(screen.queryByText("@wrong.test")).toBeNull();
  expect(screen.getByRole("button", { name: `Remove Verifier: ${issuer}` })).toBeTruthy();
});

it("uses the handle for a saved verifier without a display name or avatar", async () => {
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] }));
  render(<App />);
  expect(await screen.findByText("verifier.test")).toBeTruthy();
  expect(screen.getByText("@verifier.test")).toBeTruthy();
  expect(within(screen.getByRole("list")).queryByRole("img")).toBeNull();
});

it("ignores late saved-profile responses after switching accounts", async () => {
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + viewer, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [issuer] }));
  window.localStorage.setItem(Verification.VERIFICATION_STORAGE_PREFIX + other, JSON.stringify({ includesBlueskyVerification: true, trustedVerifierDIDs: [viewer] }));
  let finish!: (value: Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>) => void;
  const profile = spyOn(Verification, "resolveVerifierProfile").mockImplementation(async actor => actor === issuer ? new Promise(resolve => { finish = resolve; }) : { did: viewer, handle: "next.test", displayName: "Next Verifier" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); restores.push(() => profile.mockRestore());
  const view = render(<App />);
  await waitFor(() => expect(finish).toBeDefined());
  did = other; view.rerender(<App />);
  expect(await screen.findByText("Next Verifier")).toBeTruthy();
  await act(async () => { finish({ did: issuer, handle: "old.test", displayName: "Old Verifier" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>); });
  expect(screen.queryByText("Old Verifier")).toBeNull();
  expect(screen.getByRole("button", { name: "Remove Verifier: @next.test" })).toBeTruthy();
  expect(profile.mock.calls.find(([actor]) => actor === issuer)?.[1]?.aborted).toBe(true);
});
