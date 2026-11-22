import { describe, expect, it } from "bun:test";
import { GET, HEAD } from "@/app/podcast-clips/[...path]/route";

describe("retired podcast clip links", () => {
  it("return 410 Gone without indexing", async () => {
    for (const handler of [GET, HEAD]) {
      const response = handler();
      expect(response.status).toBe(410);
      expect(response.headers.get("X-Robots-Tag")).toBe("noindex");
    }
    expect(await GET().text()).toContain("no longer available");
  });
});
