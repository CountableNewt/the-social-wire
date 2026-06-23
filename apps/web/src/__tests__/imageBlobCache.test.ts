import { describe, expect, it } from "bun:test";

import {
  isDirectImageLoadUrl,
  resolveDirectImageUrl,
  shouldUseDirectImageSrc,
} from "@/lib/imageBlobCache";

describe("image direct src resolution", () => {
  it("uses direct img src for cross-origin CDN hosts without CORS", () => {
    const url =
      "https://cdn.bsky.app/img/avatar/plain/did:plc:abc/bafy";
    expect(isDirectImageLoadUrl(url)).toBe(true);
    expect(shouldUseDirectImageSrc(url)).toBe(true);
    expect(resolveDirectImageUrl(url)).toBe(url);
  });
});
