import { afterEach, beforeEach, expect, test } from "bun:test";
import { assertPostSchedulingEnabled, isPostSchedulingEnabled } from "@/lib/postScheduling";

let previous: string | undefined;
beforeEach(() => { previous = process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; });
afterEach(() => { if (previous === undefined) delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING; else process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = previous; });

test("requires an explicit true opt-in and defaults off", () => {
  delete process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING;
  expect(isPostSchedulingEnabled()).toBe(false);
  for (const value of ["false", "TRUE", "1", "", " true "]) {
    process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = value;
    expect(isPostSchedulingEnabled()).toBe(false);
    expect(assertPostSchedulingEnabled).toThrow();
  }
  process.env.NEXT_PUBLIC_ENABLE_POST_SCHEDULING = "true";
  expect(isPostSchedulingEnabled()).toBe(true);
  expect(assertPostSchedulingEnabled).not.toThrow();
});
