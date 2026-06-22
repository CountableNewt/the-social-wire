import { describe, expect, it } from "bun:test";

import { articleFallbackContentIsVerified } from "@/lib/articleFallbackVerification";

describe("articleFallbackVerification", () => {
  it("requires matching head AT URI for standard.site entry fallbacks", () => {
    expect(
      articleFallbackContentIsVerified({
        expectedAtUri: "at://did:plc:author/site.standard.document/post",
        pageAtUri: "at://did:plc:author/site.standard.document/post",
      })
    ).toBe(true);
    expect(
      articleFallbackContentIsVerified({
        expectedAtUri: "at://did:plc:author/site.standard.document/post",
        pageAtUri: "at://did:plc:other/site.standard.document/post",
      })
    ).toBe(false);
    expect(
      articleFallbackContentIsVerified({
        expectedAtUri: "at://did:plc:author/site.standard.document/post",
      })
    ).toBe(false);
  });

  it("allows Skyreader RSS fallback content without page AT URI metadata", () => {
    expect(
      articleFallbackContentIsVerified({
        expectedAtUri: "rssentry:aHR0cHM6Ly9leGFtcGxlLmNvbS9mZWVkLnhtbA",
      })
    ).toBe(true);
  });
});
