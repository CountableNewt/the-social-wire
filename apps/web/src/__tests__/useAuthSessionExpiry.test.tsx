import { useEffect } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, spyOn } from "bun:test";
import type { OAuthSession } from "@atproto/oauth-client-browser";

import { AuthProvider, useAuth } from "@/hooks/useAuth";
import * as Auth from "@/lib/auth";

const did = "did:plc:expiry-viewer";
let apply: ReturnType<typeof useAuth>["applyOAuthSession"];
const restores: (() => void)[] = [];
function AuthStateProbe() {
  const auth = useAuth();
  useEffect(() => { apply = auth.applyOAuthSession; }, [auth.applyOAuthSession]);
  return <p>{auth.session ? `signed-in:${auth.session.did}` : "signed-out"}</p>;
}
beforeEach(() => {
  const callback = spyOn(Auth, "pathnameIsOAuthCallbackRoute").mockReturnValue(true);
  restores.push(() => callback.mockRestore());
});
afterEach(() => { cleanup(); restores.splice(0).forEach(restore => restore()); });
async function mountSignedIn() {
  render(<AuthProvider><AuthStateProbe /></AuthProvider>);
  await act(async () => apply({ did, fetchHandler: globalThis.fetch } as unknown as OAuthSession));
}
it("clears provider auth state when the OAuth session expires", async () => {
  await mountSignedIn();
  expect(screen.getByText(`signed-in:${did}`)).toBeTruthy();
  await act(async () => { Auth.invalidateOAuthSession(did, new Error("expired")); });
  expect(screen.getByText("signed-out")).toBeTruthy();
});
it("does not sign out the personal provider when a linked account expires", async () => {
  await mountSignedIn();
  await act(async () => { Auth.invalidateOAuthSession("did:plc:linked-brand", new Error("expired")); });
  expect(screen.getByText(`signed-in:${did}`)).toBeTruthy();
});
