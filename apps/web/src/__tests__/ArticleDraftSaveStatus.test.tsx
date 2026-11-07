import { afterEach, expect, it } from "bun:test";
import { cleanup, render, screen } from "@testing-library/react";
import { ArticleDraftSaveStatus } from "@/components/Articles/ArticleDraftSaveStatus";

afterEach(cleanup);

it("keeps a compact fixed status slot through saving and long sync messages", () => {
  const view = render(<ArticleDraftSaveStatus status="Saved in This Browser" syncing={false} />);
  const slot = screen.getByRole("status");
  const slotClasses = slot.className;
  for (const [status, syncing, label] of [
    ["Saving…", false, "Saving"],
    ["Saved Locally · Syncing…", true, "Syncing"],
    ["Synced Privately", false, "Synced"],
    ["Saved Locally · Sync Pending", false, "Pending"],
    ["Unsaved Changes", false, "Unsaved"],
    ["Not Saved", false, "Not Saved"],
  ] as const) {
    view.rerender(<ArticleDraftSaveStatus status={status} syncing={syncing} />);
    expect(slot.className).toBe(slotClasses);
    expect(slot.classList.contains("w-24")).toBe(true);
    expect(slot.classList.contains("h-6")).toBe(true);
    expect(slot.classList.contains("shrink-0")).toBe(true);
    expect(slot.classList.contains("whitespace-nowrap")).toBe(true);
    expect(slot.getAttribute("aria-label")).toBe(status);
    expect(slot.title).toBe(status);
    expect(slot.textContent).toBe(label);
    expect(slot.querySelector("svg")?.classList.contains("size-3")).toBe(true);
  }
});

it("limits saving animation to the small icon and respects reduced motion", () => {
  const view = render(<ArticleDraftSaveStatus status="Saving…" syncing={false} />);
  expect(screen.getByRole("status").querySelector("svg")?.classList.contains("motion-safe:animate-spin")).toBe(true);
  expect(screen.getByRole("status").className).not.toContain("animate");
  view.rerender(<ArticleDraftSaveStatus status="Synced Privately" syncing={false} />);
  expect(screen.getByRole("status").querySelector("svg")?.className.baseVal).not.toContain("animate-spin");
});
