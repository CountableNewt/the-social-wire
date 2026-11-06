"use client";

import { Check, CircleAlert, Clock, LoaderCircle } from "lucide-react";

export function ArticleDraftSaveStatus({ status, syncing }: { status: string; syncing: boolean }) {
  const pending = status.includes("Pending");
  const failed = status === "Not Saved";
  const unsaved = status === "Unsaved Changes";
  const active = !pending && !failed && (syncing || status.startsWith("Saving") || status.startsWith("Loading"));
  const label = failed ? "Not Saved" : pending ? "Pending" : unsaved ? "Unsaved" : status.startsWith("Loading") ? "Loading" : status.startsWith("Saving") ? "Saving" : active ? "Syncing" : status === "Synced Privately" ? "Synced" : "Saved";
  const Icon = failed ? CircleAlert : pending ? Clock : active ? LoaderCircle : unsaved ? Clock : Check;
  return <span role="status" aria-label={status} aria-atomic="true" title={status} className="mr-auto inline-flex h-6 w-24 shrink-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs text-muted-foreground">
    <Icon aria-hidden="true" className={`size-3 shrink-0${active ? " motion-safe:animate-spin" : ""}`} />
    <span aria-hidden="true" className="truncate">{label}</span>
  </span>;
}
