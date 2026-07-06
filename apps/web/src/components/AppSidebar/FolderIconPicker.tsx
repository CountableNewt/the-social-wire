"use client";

import { cn } from "@/lib/utils";
import { folderIconOptions } from "./FolderIcon";

export function FolderIconPicker({
  labelledBy,
  value,
  onChange,
}: {
  labelledBy: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const trimmed = value.trim();
  const selected = folderIconOptions.some(
    (option) => option.value === trimmed
  )
    ? trimmed
    : "folder";
  const selectedClass =
    "border-[var(--purple-border-strong)] bg-[var(--purple-surface)] text-[var(--purple-foreground)] [box-shadow:var(--purple-sidebar-selected)] hover:bg-[var(--purple-surface)] hover:text-[var(--purple-foreground)]";

  return (
    <div
      role="radiogroup"
      aria-labelledby={labelledBy}
      className="grid max-h-[7.75rem] grid-cols-5 gap-1.5 overflow-y-auto overscroll-contain pr-1"
    >
      {folderIconOptions.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={selected === option.value}
          title={option.label}
          onClick={() => onChange(option.value)}
          className={cn(
            "flex h-9 items-center justify-center rounded-lg border border-border/70 bg-background/35 text-muted-foreground transition hover:bg-accent hover:text-accent-foreground",
            selected === option.value && selectedClass
          )}
        >
          <option.Icon className="h-4 w-4" aria-hidden />
          <span className="sr-only">{option.label}</span>
        </button>
      ))}
    </div>
  );
}
