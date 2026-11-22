import { describe, expect, it } from "bun:test";
import { resolveSocialWireApiOrigin, socialWireApiOrigin } from "@/lib/socialWireApiOrigin";

describe("Social Wire API origin", () => {
  it("keeps previews, Development and Production on their own deployment", () => {
    for (const origin of ["https://branch-social-wire.vercel.app", "https://testing.thesocialwire.app", "https://thesocialwire.app"]) {
      expect(resolveSocialWireApiOrigin(origin, "https://api.thesocialwire.app")).toBe(origin);
      expect(resolveSocialWireApiOrigin(origin)).toBe(origin);
    }
  });

  it("supports explicit local backends and same-origin local Services", () => {
    for (const origin of ["http://localhost:3000", "http://127.0.0.1:3000", "http://[::1]:3000"]) {
      expect(resolveSocialWireApiOrigin(origin, "http://localhost:8081/")).toBe("http://localhost:8081");
      expect(resolveSocialWireApiOrigin(origin)).toBe(origin);
    }
    expect(resolveSocialWireApiOrigin(undefined, "http://localhost:8081")).toBe("http://localhost:8081");
  });

  it("never defaults server-side callers to Production", () => {
    expect(() => resolveSocialWireApiOrigin()).toThrow("requires a browser origin");
    for (const override of ["https://api.example/path", "https://user:secret@api.example", "file:///tmp/api"]) {
      expect(() => resolveSocialWireApiOrigin("http://localhost:3000", override)).toThrow("HTTP(S) origin");
    }
  });

  it("resolves server rendering to this Vercel deployment despite stale API env", () => {
    const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
    Object.defineProperty(globalThis, "window", { configurable: true, value: undefined });
    const previousDeployment = process.env.VERCEL_URL;
    const previousAPI = process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL;
    process.env.VERCEL_URL = "isolated-social-wire.vercel.app";
    process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL = "https://api.thesocialwire.app";
    try {
      expect(socialWireApiOrigin()).toBe("https://isolated-social-wire.vercel.app");
    } finally {
      if (originalWindow) Object.defineProperty(globalThis, "window", originalWindow);
      else Reflect.deleteProperty(globalThis, "window");
      if (previousDeployment === undefined) delete process.env.VERCEL_URL;
      else process.env.VERCEL_URL = previousDeployment;
      if (previousAPI === undefined) delete process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL;
      else process.env.NEXT_PUBLIC_SOCIALWIRE_API_URL = previousAPI;
    }
  });
});
