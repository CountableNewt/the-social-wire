import { describe, expect, it } from "bun:test";
import {
  legacyIOSLatrExternalRkey,
  isLegacyLatrExternalRkey,
} from "@/lib/legacyRecordKeys";

describe("legacyRecordKeys", () => {
  it("detects legacy iOS external rkeys", async () => {
    const canonical = "MMSTQKIENDT2HHAGGI6J4OXJR4YQOLLEDS5TP2RXSF7VNO7LKU4Q";
    const legacy = await legacyIOSLatrExternalRkey("https://example.com/article");
    expect(isLegacyLatrExternalRkey(canonical, legacy)).toBe(true);
    expect(legacy).toBe("mmstqkiendt2hhaggi6j4oxjr4yqolleds5tp2rxsf7vno7lku4q");
  });

});
