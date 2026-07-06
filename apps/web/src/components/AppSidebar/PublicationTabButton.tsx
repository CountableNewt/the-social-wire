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
        "flex h-8 min-h-8 min-w-0 items-center justify-center rounded-xl px-3 py-0 text-center text-xs font-semibold transition-[background-color,border-color,box-shadow,color] backdrop-blur-sm",
        active
          ? "border border-[var(--purple-border)] bg-primary text-primary-foreground [box-shadow:var(--purple-sidebar-selected)] hover:bg-primary hover:text-primary-foreground hover:[box-shadow:var(--purple-sidebar-selected)]"
          : "border border-transparent bg-transparent text-muted-foreground hover:border-[var(--purple-border)] hover:bg-sidebar-accent/50 hover:text-sidebar-foreground hover:[box-shadow:var(--purple-glow-hover)] dark:hover:bg-sidebar-accent/38"
      )}
    >
      <span className="block truncate">{children}</span>
    </button>
  );
}
