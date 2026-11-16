"use client";

import { useRef, useState } from "react";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { PublishTimeField } from "./PublishTimeField";
import { localDateTimeValue, scheduledInstant } from "./calendarDates";

export function RescheduleDialog({ initialTime, save, close }: { initialTime: string; save: (instant: string) => Promise<void>; close: () => void }) {
  const busy = useRef(false);
  const [value, setValue] = useState(() => { const date = new Date(initialTime); return Number.isFinite(date.getTime()) ? localDateTimeValue(date) : ""; });
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  async function submit() {
    const instant = scheduledInstant(value, undefined, initialTime);
    if (!instant || busy.current) return;
    busy.current = true;
    setPending(true); setError(undefined);
    try { await save(instant); close(); } catch (failure) { setError(failure instanceof Error ? failure.message : "Schedule Could Not Be Updated."); } finally { busy.current = false; setPending(false); }
  }
  return <Dialog open onOpenChange={open => { if (!open && !pending) close(); }}><DialogContent showCloseButton={!pending}><DialogTitle>Reschedule Post</DialogTitle><DialogDescription>Choose a new publish time in your browser’s time zone.</DialogDescription><form className="space-y-3" onSubmit={event => { event.preventDefault(); void submit(); }}><PublishTimeField value={value} originalTime={initialTime} disabled={pending} onChange={setValue} />{error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}<DialogFooter><DialogClose disabled={pending} className="rounded-xl border px-3 py-2">Cancel</DialogClose><Button type="submit" disabled={pending || !scheduledInstant(value, undefined, initialTime)}>{pending ? "Saving…" : "Save Time"}</Button></DialogFooter></form></DialogContent></Dialog>;
}
