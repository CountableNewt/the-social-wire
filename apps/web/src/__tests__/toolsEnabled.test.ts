import { afterEach, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { NextRequest } from "next/server";
import { config, proxy } from "@/proxy";

const env: Record<string, string | undefined> = process.env;
const keys = ["NEXT_PUBLIC_LIVESTREAM_ENABLED", "NEXT_PUBLIC_APP_ENV", "APP_ENV", "NEXT_PUBLIC_ENABLE_POST_SCHEDULING", "NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED"] as const;
const original = Object.fromEntries(keys.map(key => [key, env[key]]));
afterEach(() => { for (const key of keys) { if (original[key] === undefined) delete env[key]; else env[key] = original[key]; } });

test("server proxy covers workspace routes without intercepting Social or Reader", () => {
  const tools = ["articles", "live-post", "livestream", "media", "brand-accounts", "content-calendar", "analytics", "scheduling"];
  const matched = tools.flatMap(path => ["", "/", "/nested", "?_rsc=transition"].map(suffix => `/${path}${suffix}`));
  const allowed = ["/social", "/social/messages", "/podcast-clips/one", "/read", "/me", "/articles-other"];
  const script = `
    const { unstable_doesMiddlewareMatch } = require("next/experimental/testing/server");
    const { config, paths } = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
    process.stdout.write(JSON.stringify(paths.map(path => unstable_doesMiddlewareMatch({ config, url: "https://thesocialwire.app" + path }))));
  `;
  const matches = JSON.parse(execFileSync(process.execPath, ["-e", script], {
    input: JSON.stringify({ config, paths: [...matched, ...allowed] }), encoding: "utf8",
  }));
  expect(matches).toEqual([...matched.map(() => true), ...allowed.map(() => false)]);
});

for (const environment of ["prod", "dev", "local"]) test(`live workspaces pass through in ${environment}`, () => {
  env.NEXT_PUBLIC_APP_ENV = environment;
  for (const section of ["articles", "analytics", "live-post"]) {
    const response = proxy(new NextRequest(`https://thesocialwire.app/${section}/nested`));
    expect(response.headers.get("x-middleware-next")).toBe("1");
  }
});

for (const section of ["content-calendar", "scheduling"]) test(`${section} retains its scheduling availability flag`, () => {
  const request = new NextRequest(`https://thesocialwire.app/${section}/nested`);
  for (const flag of [undefined, "false", "TRUE", "1"]) {
    if (flag === undefined) delete env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
    else env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = flag;
    expect(proxy(request).status).toBe(404);
  }
  env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true";
  expect(proxy(request).headers.get("x-middleware-next")).toBe("1");
});

test("Delegated Accounts retains its independent availability flag", () => {
  const request = new NextRequest("https://thesocialwire.app/brand-accounts/nested");
  delete env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
  expect(proxy(request).status).toBe(404);
  env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true";
  expect(proxy(request).headers.get("x-middleware-next")).toBe("1");
});

test("Livestream can stay disabled while live tools remain available", () => {
  env.NEXT_PUBLIC_LIVESTREAM_ENABLED = "false";
  const response = proxy(new NextRequest("https://thesocialwire.app/livestream/nested"));
  expect(response.status).toBe(404);
  expect(response.headers.get("Cache-Control")).toBe("private, no-store");
  expect(proxy(new NextRequest("https://thesocialwire.app/live-post")).headers.get("x-middleware-next")).toBe("1");
  env.NEXT_PUBLIC_LIVESTREAM_ENABLED = "true";
  expect(proxy(new NextRequest("https://thesocialwire.app/livestream/nested")).headers.get("x-middleware-next")).toBe("1");
});

test("retired Media routes stay unavailable", () => {
  for (const path of ["media", "media/photos", "media/short-videos"]) {
    expect(proxy(new NextRequest(`https://thesocialwire.app/${path}`)).status).toBe(404);
  }
});

test("Livestream defaults to Development only when no deployment flag is supplied", () => {
  delete env.NEXT_PUBLIC_LIVESTREAM_ENABLED;
  env.NEXT_PUBLIC_APP_ENV = "prod";
  expect(proxy(new NextRequest("https://thesocialwire.app/livestream")).status).toBe(404);
  env.NEXT_PUBLIC_APP_ENV = "dev";
  expect(proxy(new NextRequest("https://testing.thesocialwire.app/livestream")).headers.get("x-middleware-next")).toBe("1");
});
