"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

export function ArticlePanelResizeHandle({ side, width, onWidthChange }: {
  side: "drafts" | "metadata";
  width: number;
  onWidthChange: (width: number) => void;
}) {
  const drag = useRef<{ x: number; width: number } | null>(null);
  const handle = useRef<HTMLDivElement>(null);
  const [maximum, setMaximum] = useState(480);
  const minimum = Math.min(208, maximum);
  useEffect(() => {
    const container = handle.current?.parentElement?.parentElement;
    if (!container || typeof ResizeObserver === "undefined") return;
    const measure = () => {
      if (container.clientWidth) setMaximum(Math.min(480, Math.floor(container.clientWidth * (side === "drafts" ? 0.3 : 0.4))));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    return () => observer.disconnect();
  }, [side]);
  useEffect(() => {
    if (width > maximum) onWidthChange(maximum);
    else if (width < minimum) onWidthChange(minimum);
  }, [maximum, minimum, width, onWidthChange]);
  const direction = side === "drafts" ? 1 : -1;
  const update = (value: number) => onWidthChange(Math.max(minimum, Math.min(maximum, value)));
  return <div
    ref={handle}
    role="separator"
    aria-label={side === "drafts" ? "Resize Drafts" : "Resize Metadata"}
    aria-orientation="vertical"
    aria-valuemin={minimum}
    aria-valuemax={maximum}
    aria-valuenow={Math.min(width, maximum)}
    aria-controls={side === "drafts" ? "article-library" : "article-metadata"}
    tabIndex={0}
    className={cn("absolute inset-y-2 z-10 hidden w-3 touch-none cursor-col-resize select-none rounded hover:bg-primary/15 focus-visible:bg-primary/15 focus-visible:outline-ring", side === "drafts" ? "-right-2 md:block" : "-left-2 lg:block")}
    onPointerDown={event => {
      if (event.button !== 0) return;
      event.preventDefault();
      event.currentTarget.setPointerCapture(event.pointerId);
      drag.current = { x: event.clientX, width: event.currentTarget.parentElement?.getBoundingClientRect().width || width };
    }}
    onPointerMove={event => {
      if (drag.current) update(drag.current.width + direction * (event.clientX - drag.current.x));
    }}
    onPointerUp={event => {
      drag.current = null;
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    }}
    onPointerCancel={() => { drag.current = null; }}
    onLostPointerCapture={() => { drag.current = null; }}
    onKeyDown={event => {
      const step = event.shiftKey ? 24 : 8;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        update(width + direction * (event.key === "ArrowRight" ? step : -step));
      } else if (event.key === "Home" || event.key === "End") {
        event.preventDefault();
        update(event.key === "Home" ? minimum : maximum);
      }
    }}
  />;
}
