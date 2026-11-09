import { afterEach, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { NextRequest } from "next/server";
import { toolsEnabled } from "@/lib/toolsEnabled";
import { config, proxy } from "@/proxy";

const env: Record<string, string | undefined> = process.env;
const keys = ["NEXT_PUBLIC_TOOLS_ENABLED", "NEXT_PUBLIC_APP_ENV", "APP_ENV", "NODE_ENV", "NEXT_PUBLIC_ENABLE_POST_SCHEDULING", "NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED"] as const;
const original = Object.fromEntries(keys.map(key => [key, env[key]]));
afterEach(() => { for (const key of keys) { if (original[key] === undefined) delete env[key]; else env[key] = original[key]; } });

function environment(value: string) {
  delete process.env.NEXT_PUBLIC_TOOLS_ENABLED;
  delete process.env.APP_ENV;
  process.env.NEXT_PUBLIC_APP_ENV = value;
}

test("Tools defaults closed in Production and unknown environments, open only in known nonproduction environments", () => {
  for (const value of ["prod", "production", "preview-unknown"]) { environment(value); expect(toolsEnabled()).toBe(false); }
  for (const value of ["dev", "local", "test"]) { environment(value); expect(toolsEnabled()).toBe(true); }
  environment(""); env.NODE_ENV = "production";
  expect(toolsEnabled()).toBe(false);
});

test("an explicit deployment value overrides defaults and malformed values fail closed", () => {
  environment("dev");
  for (const value of ["false", "", "TRUE", "1"]) { process.env.NEXT_PUBLIC_TOOLS_ENABLED = value; expect(toolsEnabled()).toBe(false); }
  environment("prod"); process.env.NEXT_PUBLIC_TOOLS_ENABLED = "true";
  expect(toolsEnabled()).toBe(true);
});

test("server proxy covers all Tools trees and callbacks without intercepting Social, Podcasts or Reader", () => {
  const tools = ["articles", "live-post", "livestream", "brand-accounts", "content-calendar", "analytics", "scheduling"];
  const blocked = tools.flatMap(path => ["", "/", "/nested", "?_rsc=transition"].map(suffix => `/${path}${suffix}`));
  const allowed = ["/social", "/social/messages", "/podcasts", "/podcast-clips/one", "/read", "/me", "/articles-other"];
  // Next's testing package initializes server globals. Keep it out of the shared
  // DOM test process so later route tests retain their own async storage setup.
  // Next 16.2.6 still exports this helper under its Middleware name.
  const script = `
    const { unstable_doesMiddlewareMatch } = require("next/experimental/testing/server");
    const { config, paths } = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
    process.stdout.write(JSON.stringify(paths.map(path => unstable_doesMiddlewareMatch({ config, url: "https://thesocialwire.app" + path }))));
  `;
  const matches = JSON.parse(execFileSync(process.execPath, ["-e", script], {
    input: JSON.stringify({ config, paths: [...blocked, ...allowed] }), encoding: "utf8",
  }));
  expect(matches).toEqual([...blocked.map(() => true), ...allowed.map(() => false)]);
});

test("Production Tools requests return an uncached 404 while Development passes through", () => {
  environment("prod");
  const denied = proxy(new NextRequest("https://thesocialwire.app/live-post"));
  expect(denied.status).toBe(404);
  expect(denied.headers.get("Cache-Control")).toBe("private, no-store");
  expect(denied.headers.get("x-middleware-next")).toBeNull();
  environment("dev");
  expect(proxy(new NextRequest("https://thesocialwire.app/live-post")).headers.get("x-middleware-next")).toBe("1");
  process.env.NEXT_PUBLIC_TOOLS_ENABLED = "false";
  expect(proxy(new NextRequest("https://thesocialwire.app/live-post")).status).toBe(404);
});

for (const path of ["/articles", "/articles/", "/articles/new", "/articles/draft/example", "/articles?_rsc=transition"]) test(`Articles bypasses the broad Tools flag at ${path}`, () => {
  environment("prod");
  process.env.NEXT_PUBLIC_TOOLS_ENABLED = "false";
  delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
  const response = proxy(new NextRequest("https://thesocialwire.app" + path));
  expect(response.headers.get("x-middleware-next")).toBe("1");
});
for (const section of ["content-calendar", "scheduling"]) test(`${section} requires both scheduling and Tools availability for direct and nested requests`, () => {
  for (const suffix of ["", "/", "/nested", "?_rsc=transition"]) {
    const request = new NextRequest(`https://thesocialwire.app/${section}${suffix}`);
    environment("dev");
    for (const flag of [undefined, "false", "TRUE", "1"]) {
      if (flag === undefined) delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
      else process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = flag;
      const denied = proxy(request);
      expect(denied.status).toBe(404);
      expect(denied.headers.get("Cache-Control")).toBe("private, no-store");
    }
    process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true";
    expect(proxy(request).headers.get("x-middleware-next")).toBe("1");
    process.env.NEXT_PUBLIC_TOOLS_ENABLED = "false";
    expect(proxy(request).status).toBe(404);
  }
});
for (const section of ["live-post", "livestream", "analytics"]) test(`${section} retains the deployment Tools gate`, () => {
  const request = new NextRequest(`https://thesocialwire.app/${section}/nested`);
  environment("prod");
  process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true";
  expect(proxy(request).status).toBe(404);
  process.env.NEXT_PUBLIC_TOOLS_ENABLED = "true";
  expect(proxy(request).headers.get("x-middleware-next")).toBe("1");
});


test("Brand Accounts stays unavailable unless both its flag and Tools are enabled", () => {
  environment("dev");
  for (const suffix of ["", "/", "/nested", "?_rsc=transition"]) {
    const request = new NextRequest(`https://thesocialwire.app/brand-accounts${suffix}`);
    for (const flag of [undefined, "false", "", "TRUE", "1"]) {
      if (flag === undefined) delete process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED;
      else process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = flag;
      const response = proxy(request);
      expect(response.status).toBe(404);
      expect(response.headers.get("Cache-Control")).toBe("private, no-store");
    }
    process.env.NEXT_PUBLIC_BRAND_ACCOUNTS_ENABLED = "true";
    expect(proxy(request).headers.get("x-middleware-next")).toBe("1");
  }
  process.env.NEXT_PUBLIC_TOOLS_ENABLED = "false";
  expect(proxy(new NextRequest("https://thesocialwire.app/brand-accounts")).status).toBe(404);
});
