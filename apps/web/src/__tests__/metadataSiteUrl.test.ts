import { expect, test } from "bun:test";

import { metadataSiteUrl } from "@/lib/metadataSiteUrl";

test("metadata prefers the explicit stable domain over a Vercel preview", () => {
  expect(metadataSiteUrl({ NEXT_PUBLIC_SITE_URL: " https://testing.thesocialwire.app/ ", VERCEL_URL: "preview.vercel.app" }))
    .toBe("https://testing.thesocialwire.app");
});

test("metadata uses the current Vercel deployment only without an explicit origin", () => {
  expect(metadataSiteUrl({ VERCEL_URL: "preview.vercel.app" })).toBe("https://preview.vercel.app");
  expect(metadataSiteUrl({ RAILWAY_PUBLIC_DOMAIN: "retired-host.up.railway.app" })).toBe("https://thesocialwire.app");
});

test("invalid site configuration fails without echoing its value", () => {
  for (const value of ["not a URL", "https://user:secret@example.com", "javascript:alert(1)", "https://example.com/path", "https://example.com/?token=secret"]) {
    expect(() => metadataSiteUrl({ NEXT_PUBLIC_SITE_URL: value })).toThrow("Metadata Requires a Valid Public Site Origin.");
  }
});
