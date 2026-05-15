import { describe, expect, it } from "bun:test";
import {
  localLoopbackCanonicalHref,
  localOAuthCanonicalHref,
  pathnameIsOAuthCallbackRoute,
} from "@/lib/auth";

describe("pathnameIsOAuthCallbackRoute", () => {
  it("matches default /callback", () => {
    expect(pathnameIsOAuthCallbackRoute("/callback")).toBe(true);
  });

  it("does not treat other routes as callback path", () => {
    expect(pathnameIsOAuthCallbackRoute("/read")).toBe(false);
    expect(pathnameIsOAuthCallbackRoute("/login")).toBe(false);
  });
});

describe("localOAuthCanonicalHref", () => {
  const clientId =
    "http://localhost?redirect_uri=http%3A%2F%2F127.0.0.1%3A3000%2Fcallback";

  it("moves local sign-in pages from localhost to the callback loopback IP", () => {
    expect(
      localOAuthCanonicalHref(
        "http://localhost:3000/login?error=callback_failed#retry",
        clientId,
        ["http://127.0.0.1:3000/callback"]
      )
    ).toBe("http://127.0.0.1:3000/login?error=callback_failed#retry");
  });

  it("does not rewrite hosted client IDs", () => {
    expect(
      localOAuthCanonicalHref(
        "http://localhost:3000/login",
        "https://thesocialwire.app/client-metadata.json",
        ["https://thesocialwire.app/callback"]
      )
    ).toBeNull();
  });

  it("does not rewrite when already on loopback IP", () => {
    expect(
      localOAuthCanonicalHref(
        "http://127.0.0.1:3000/login",
        clientId,
        ["http://127.0.0.1:3000/callback"]
      )
    ).toBeNull();
  });
});

describe("localLoopbackCanonicalHref", () => {
  it("rewrites localhost to 127.0.0.1 without changing path or params", () => {
    expect(
      localLoopbackCanonicalHref("http://localhost:3000/login?x=1#frag")
    ).toBe("http://127.0.0.1:3000/login?x=1#frag");
  });

  it("does not rewrite already canonical loopback IP URLs", () => {
    expect(localLoopbackCanonicalHref("http://127.0.0.1:3000/login")).toBeNull();
  });
});
