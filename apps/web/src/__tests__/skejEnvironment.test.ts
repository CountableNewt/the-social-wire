import { expect, test } from "bun:test";
import { AT_PROTO_OAUTH_SCOPES } from "@/lib/atprotoOAuthScopes";
import { BRAND_ACCOUNT_OAUTH_SCOPES } from "@/lib/brandAccounts/brandAccountScopes";
import { getSkejEnvironment, SKEJ_SCHEDULING_METHODS } from "@/lib/skej/skejEnvironment";

test("scheduling uses the Social Wire edge in every environment", () => {
  const original = process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL;
  process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL = "https://api.testing.thesocialwire.app";
  try {
    expect(getSkejEnvironment()).toEqual({ api: "https://api.testing.thesocialwire.app", connectUrl: "https://api.testing.thesocialwire.app/oauth/scheduling/start" });
  } finally {
    if (original === undefined) delete process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL;
    else process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL = original;
  }
});

test("personal and brand consent omit external scheduler service scopes", () => {
  expect(SKEJ_SCHEDULING_METHODS).toHaveLength(5);
  for (const scopes of [AT_PROTO_OAUTH_SCOPES, BRAND_ACCOUNT_OAUTH_SCOPES]) {
    expect(scopes).not.toContain("skej");
    expect(scopes).not.toContain("rpc:com.atproto.server.getServiceAuth");
  }
});
