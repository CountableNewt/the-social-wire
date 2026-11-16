import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import * as Context from "@/contexts/SocialVerificationContext";
import * as Verification from "@/lib/socialVerification";
import { SocialVerificationBadge } from "@/components/Social/SocialVerificationBadge";

const issuer = "did:plc:abcdefghijklmnopqrstuvwx";
const other = "did:plc:bcdefghijklmnopqrstuvwxy";
const subject = { did: other, handle: "person.test", displayName: "Person", verification: {
  verifiedStatus: "valid", trustedVerifierStatus: "none", verifications: [
    { issuer, uri: `at://${issuer}/app.bsky.graph.verification/one`, isValid: true, createdAt: "2026-11-07T00:00:00Z" },
    { issuer: other, uri: `at://${other}/app.bsky.graph.verification/two`, isValid: false, createdAt: "2026-11-07T00:00:00Z" },
  ],
} };
let client: QueryClient;
const restores: (() => void)[] = [];
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  const context = spyOn(Context, "useSocialVerification").mockReturnValue({ preferences: Verification.DEFAULT_VERIFICATION_PREFERENCES, records: [], available: true, loading: false, error: undefined, update: () => {}, retry: () => {} });
  const profile = spyOn(Verification, "resolveVerifierProfile").mockResolvedValue({ did: issuer, handle: "verifier.test", displayName: "Example Verifier" } as Awaited<ReturnType<typeof Verification.resolveVerifierProfile>>);
  restores.push(() => context.mockRestore(), () => profile.mockRestore());
  for (const [name, value] of Object.entries({ getComputedStyle: window.getComputedStyle.bind(window), PointerEvent: window.MouseEvent, requestAnimationFrame: (callback: FrameRequestCallback) => setTimeout(() => callback(performance.now()), 0), cancelAnimationFrame: clearTimeout })) {
    const original = Object.getOwnPropertyDescriptor(globalThis, name);
    Object.defineProperty(globalThis, name, { configurable: true, value });
    restores.push(() => { if (original) Object.defineProperty(globalThis, name, original); else Reflect.deleteProperty(globalThis, name); });
  }
});
afterEach(() => { cleanup(); client.clear(); restores.splice(0).reverse().forEach(restore => restore()); });
function badge(value = subject) { return render(<QueryClientProvider client={client}><SocialVerificationBadge subject={value} /></QueryClientProvider>); }

it("shows valid verifier identities on keyboard focus and dismisses with Escape", async () => {
  badge();
  expect(Verification.resolveVerifierProfile).not.toHaveBeenCalled();
  const trigger = screen.getByRole("img", { name: "Verified on Bluesky" });
  expect(trigger.tabIndex).toBe(0);
  fireEvent.focus(trigger);
  const tooltip = await screen.findByRole("tooltip");
  expect(trigger.getAttribute("aria-describedby")).toBe(tooltip.id);
  await waitFor(() => expect(tooltip.textContent).toContain("Example Verifier @verifier.test"));
  expect(Verification.resolveVerifierProfile).toHaveBeenCalledTimes(1);
  expect(Verification.resolveVerifierProfile).toHaveBeenCalledWith(issuer, expect.any(AbortSignal));
  expect(tooltip.textContent).not.toContain(other);
  fireEvent.keyDown(trigger, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
});

it("shows a verifier DID when its profile cannot load", async () => {
  const profile = spyOn(Verification, "resolveVerifierProfile").mockRejectedValue(new Error("Unavailable")); restores.push(() => profile.mockRestore());
  badge();
  fireEvent.focus(screen.getByRole("img"));
  expect((await screen.findByRole("tooltip")).textContent).toContain(issuer);
  await waitFor(() => expect(Verification.resolveVerifierProfile).toHaveBeenCalledTimes(1));
});

it("opens on hover and includes custom verifiers alongside Bluesky issuers", async () => {
  const context = spyOn(Context, "useSocialVerification").mockReturnValue({ preferences: { includesBlueskyVerification: true, trustedVerifierDIDs: [other] }, records: [{ issuer: other, subject: subject.did, handle: subject.handle, displayName: subject.displayName, uri: `at://${other}/app.bsky.graph.verification/custom` }], available: true, loading: false, error: undefined, update: () => {}, retry: () => {} }); restores.push(() => context.mockRestore());
  badge();
  const trigger = screen.getByRole("img");
  fireEvent.mouseEnter(trigger);
  fireEvent.mouseMove(trigger);
  const tooltip = await screen.findByRole("tooltip");
  await waitFor(() => expect(tooltip.textContent).toContain("@verifier.test"));
  expect(tooltip.textContent).toContain(other);
  expect(Verification.resolveVerifierProfile).toHaveBeenCalledTimes(2);
});

it("does not invent issuers for trusted-verifier status without verification records", async () => {
  badge({ ...subject, verification: { verifiedStatus: "none", trustedVerifierStatus: "valid", verifications: [] } });
  fireEvent.focus(screen.getByRole("img", { name: "Trusted Verifier on Bluesky" }));
  expect((await screen.findByRole("tooltip")).textContent).toContain("Recognized as a Trusted Verifier by Bluesky.");
  expect(Verification.resolveVerifierProfile).not.toHaveBeenCalled();
});
