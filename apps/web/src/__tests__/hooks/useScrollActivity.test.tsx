import { afterEach, expect, it } from "bun:test";
import { cleanup, fireEvent, renderHook } from "@testing-library/react";

import { useScrollActivity } from "@/hooks/useScrollActivity";

afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

it("reports activity only for scrolls on the referenced element within the settle window", async () => {
  const list = document.createElement("div");
  const other = document.createElement("div");
  document.body.append(list, other);
  const ref = { current: null as HTMLElement | null };
  const { result } = renderHook(() => useScrollActivity(ref, 50));
  expect(result.current()).toBe(false);

  // The container can mount after the hook (skeleton → list).
  ref.current = list;
  fireEvent.scroll(other);
  expect(result.current()).toBe(false);

  fireEvent.scroll(list);
  expect(result.current()).toBe(true);
  await new Promise(resolve => setTimeout(resolve, 70));
  expect(result.current()).toBe(false);
});
