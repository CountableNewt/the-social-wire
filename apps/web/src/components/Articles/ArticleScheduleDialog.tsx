"use client";

import { useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { PublishTimeField } from "@/components/ContentCalendar/PublishTimeField";
import { scheduledInstant } from "@/components/ContentCalendar/calendarDates";

export function ArticleScheduleDialog({ title, save, close }: { title: string; save: (instant: string) => Promise<void>; close: () => void }) {
  const [value, setValue] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const writing = useRef(false);
  async function submit() {
    const instant = scheduledInstant(value);
    if (!instant || writing.current) return;
    writing.current = true; setPending(true); setError(undefined);
    try { await save(instant); close(); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Article Could Not Be Scheduled."); }
    finally { writing.current = false; setPending(false); }
  }
  return <Dialog open onOpenChange={open => { if (!open && !pending) close(); }}><DialogContent showCloseButton={!pending}><DialogTitle>Schedule Article?</DialogTitle><DialogDescription>Skej stores the full article in a public schedule record immediately. Publication happens at the chosen time.</DialogDescription><p className="break-words text-sm">{title}</p><form className="space-y-3" onSubmit={event => { event.preventDefault(); void submit(); }}><PublishTimeField label="Publish Article At" value={value} disabled={pending} onChange={setValue} />{error ? <p role="alert">{error}</p> : null}<div className="flex gap-2"><Button type="submit" disabled={pending || !scheduledInstant(value)}>{pending ? "Scheduling…" : "Confirm Schedule"}</Button><DialogClose disabled={pending} render={<Button variant="outline" />}>Cancel</DialogClose></div></form></DialogContent></Dialog>;
}
