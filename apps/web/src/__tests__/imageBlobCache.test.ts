import { describe, expect, it } from "bun:test";

import { isDirectImageLoadUrl } from "@/lib/imageBlobCache";

describe("isDirectImageLoadUrl", () => {
  it("uses direct img src for cross-origin CDN hosts without CORS", () => {
    expect(
      isDirectImageLoadUrl(
        "https://cdn.bsky.app/img/avatar/plain/did:plc:abc/bafy"
      )
    ).toBe(true);
  });
});
