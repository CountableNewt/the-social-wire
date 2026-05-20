"use client";

import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function PublicationTabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "flex h-8 min-h-8 min-w-0 items-center justify-center rounded-lg px-3 py-0 text-center text-xs font-medium transition-[background-color,border-color,box-shadow,color] backdrop-blur-sm hover:[box-shadow:var(--purple-glow-hover)] active:[box-shadow:var(--purple-glow-selected)]",
        active
          ? "border-sidebar-border/80 bg-sidebar font-semibold text-sidebar-foreground shadow-inner dark:border-sidebar-border dark:bg-sidebar-accent/90 dark:text-sidebar-accent-foreground"
          : "border border-transparent bg-transparent text-muted-foreground hover:border-sidebar-border/55 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground dark:hover:bg-sidebar-accent/38"
      )}
    >
      <span className="block truncate">{children}</span>
    </button>
  );
}
