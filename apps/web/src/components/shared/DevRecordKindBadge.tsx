"use client";

import { isDevDebugUiEnabled } from "@/lib/appEnv";
import type { RecordKindInfo } from "@/lib/recordKindDebug";
import { cn } from "@/lib/utils";

type DevRecordKindBadgeProps = {
  info: RecordKindInfo;
  className?: string;
};

/** Dev/local-only badge showing publication or record source (standard.site, Skyreader, L@tr, …). */
export function DevRecordKindBadge({ info, className }: DevRecordKindBadgeProps) {
  if (!isDevDebugUiEnabled()) return null;

  const label = info.collection
    ? `${info.source} · ${info.collection}`
    : info.source;

  return (
    <span
      className={cn(
        "inline-flex max-w-full shrink-0 truncate rounded bg-muted/80 px-1 py-px font-mono text-[9px] font-medium leading-tight text-muted-foreground",
        className
      )}
      title={info.detail}
    >
      {label}
    </span>
  );
}
