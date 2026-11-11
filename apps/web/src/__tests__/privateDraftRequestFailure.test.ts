import { expect, it } from "bun:test";
import { privateDraftRequestFailure } from "@/lib/privateDraftRequestFailure";

it.each([
  [400, "Check the draft details and attachments"],
  [401, "Sign in again"],
  [403, "Sign in again"],
  [413, "Reduce the draft or attachment size"],
  [429, "Wait a moment"],
  [503, "Retry when the connection or draft service is available"],
] as const)("provides recovery guidance for HTTP %s", async (status, guidance) => {
  const failure = await privateDraftRequestFailure(Response.json({ error: "Rejected", message: "Draft request rejected" }, { status }), "Saving the private draft");
  expect(failure.code).toBe("Rejected");
  expect(failure.message).toContain(guidance);
  expect(failure.message).toContain(`HTTP ${status} · Rejected`);
});

it.each([null, [], "error", { message: 42, error: {} }].map(body => ({ body })))("handles missing or invalid structured error fields: %j", async ({ body }) => {
  const failure = await privateDraftRequestFailure(Response.json(body, { status: 502 }), "Saving the private draft");
  expect(failure.code).toBeUndefined();
  expect(failure.message).toContain("Saving the private draft failed: Request failed (HTTP 502)");
});

it("bounds message length and preserves gateway error-only explanations", async () => {
  const failure = await privateDraftRequestFailure(Response.json({ error: " Private draft access denied\n " }, { status: 403 }), "Loading private drafts");
  expect(failure.message).toContain("Private draft access denied (HTTP 403)");
  const long = await privateDraftRequestFailure(Response.json({ message: "x".repeat(2000) }, { status: 503 }), "Saving the private draft");
  expect(long.message).not.toContain("x".repeat(501));
});
