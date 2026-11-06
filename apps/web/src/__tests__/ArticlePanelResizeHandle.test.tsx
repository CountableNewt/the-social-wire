import { afterEach, expect, it } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { ArticlePanelResizeHandle } from "@/components/Articles/ArticlePanelResizeHandle";

afterEach(cleanup);

function Panel({ side }: { side: "drafts" | "metadata" }) {
  const [width, setWidth] = useState(288);
  return <div><aside><ArticlePanelResizeHandle side={side} width={width} onWidthChange={setWidth} /></aside></div>;
}

function pointer(target: Element, type: string, x: number) {
  const event = new window.Event(type, { bubbles: true });
  Object.defineProperties(event, { clientX: { value: x }, pointerId: { value: 1 }, button: { value: 0 } });
  fireEvent(target, event);
}

it("drags both panel edges in opposite directions and stops on cancellation", () => {
  for (const side of ["drafts", "metadata"] as const) {
    render(<Panel side={side} />);
    const handle = screen.getByRole("separator");
    Object.assign(handle, { setPointerCapture() {}, hasPointerCapture: () => false });
    pointer(handle, "pointerdown", 300);
    pointer(handle, "pointermove", 340);
    const expected = side === "drafts" ? "328" : "248";
    expect(handle.getAttribute("aria-valuenow")).toBe(expected);
    pointer(handle, "pointercancel", 340);
    pointer(handle, "pointermove", 400);
    expect(handle.getAttribute("aria-valuenow")).toBe(expected);
    cleanup();
  }
});

it("caps keyboard resizing to available container space", () => {
  const original = globalThis.ResizeObserver;
  globalThis.ResizeObserver = class {
    observe(target: Element) { Object.defineProperty(target, "clientWidth", { configurable: true, value: 800 }); this.callback([], this); }
    unobserve() {}
    disconnect() {}
    constructor(private callback: ResizeObserverCallback) {}
  } as typeof ResizeObserver;
  try {
    render(<Panel side="drafts" />);
    const handle = screen.getByRole("separator");
    expect(handle.getAttribute("aria-valuemax")).toBe("240");
    fireEvent.keyDown(handle, { key: "End" });
    expect(handle.getAttribute("aria-valuenow")).toBe("240");
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(handle.getAttribute("aria-valuenow")).toBe("232");
  } finally {
    globalThis.ResizeObserver = original;
  }
});
