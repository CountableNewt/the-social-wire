import { describe, it, expect } from "bun:test";
import {
  normalizeHttpUrlToHttps,
  sanitizeEmbedUrlForIframe,
  thumbnailImageSrcAttempts,
} from "@/lib/publicResourceUrl";

describe("normalizeHttpUrlToHttps", () => {
  it("promotes http to https", () => {
    expect(normalizeHttpUrlToHttps("http://atproto.brid.gy/xrpc/foo")).toBe(
      "https://atproto.brid.gy/xrpc/foo"
    );
  });

  it("strips bridge_completed query param", () => {
    expect(
      normalizeHttpUrlToHttps(
        "https://blog.example/post/slug?bridge_completed=1&utm_source=x"
      )
    ).toBe("https://blog.example/post/slug?utm_source=x");
  });

  it("strips bridgy bridge_* noise from production blog URLs", () => {
    expect(
      normalizeHttpUrlToHttps(
        "https://blog.stygiantech.dev/pieces/good-writing?bridge_completed=1"
      )
    ).toBe("https://blog.stygiantech.dev/pieces/good-writing");
    expect(
      normalizeHttpUrlToHttps(
        "https://blog.stygiantech.dev/a/b?utm=x&Bridge_completed=yes&bridge_foo=z"
      )
    ).toBe("https://blog.stygiantech.dev/a/b?utm=x");
  });

  it("strips Bridge_completed case-insensitively", () => {
    expect(
      normalizeHttpUrlToHttps(
        "https://blog.example/p?Bridge_completed=1&y=1"
      )
    ).toBe("https://blog.example/p?y=1");
  });

  it("delegates sanitizeEmbedUrlForIframe to normalizer", () => {
    expect(
      sanitizeEmbedUrlForIframe(
        "http://blog.stygiantech.dev/x?Bridge_completed=1"
      )
    ).toBe("https://blog.stygiantech.dev/x");
  });

  it("is idempotent for https origins", () => {
    expect(normalizeHttpUrlToHttps("https://pds.example/xrpc/a")).toBe(
      "https://pds.example/xrpc/a"
    );
  });
});

describe("thumbnailImageSrcAttempts", () => {
  it("drops Bridgy sync.getBlob when a non-bridgy candidate exists (order: HTTPS effective first)", () => {
    expect(
      thumbnailImageSrcAttempts(
        "http://atproto.brid.gy/xrpc/com.atproto.sync.getBlob?did=a&cid=b",
        "https://img.example/thumb.png"
      )
    ).toEqual(["https://img.example/thumb.png"]);
  });

  it("omits Bridgy sync.getBlob when it is the only candidate (avoid predictable 400 GETs)", () => {
    expect(
      thumbnailImageSrcAttempts(
        "https://atproto.brid.gy/xrpc/com.atproto.sync.getBlob?did=a&cid=b"
      )
    ).toEqual([]);
  });

  it("keeps ordinary PDS getBlob URLs", () => {
    expect(
      thumbnailImageSrcAttempts(
        "https://pds.example/xrpc/com.atproto.sync.getBlob?did=did%3Aplc%3Ax&cid=bafy"
      )
    ).toEqual([
      "https://pds.example/xrpc/com.atproto.sync.getBlob?did=did%3Aplc%3Ax&cid=bafy",
    ]);
  });

  it("dedupes identical primary and fallback after normalization", () => {
    expect(
      thumbnailImageSrcAttempts(
        "http://cdn.example/x",
        "https://cdn.example/x"
      )
    ).toEqual(["https://cdn.example/x"]);
  });
});
