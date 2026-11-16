"use client";

import { PublishTimeField } from "@/components/ContentCalendar/PublishTimeField";

export function ScheduleTimingControls({ mode, value, disabled, onModeChange, onChange }: { mode: "now" | "schedule"; value: string; disabled: boolean; onModeChange: (mode: "now" | "schedule") => void; onChange: (value: string) => void }) {
  return <div className="space-y-2">
    <div className="flex gap-2" aria-label="Publishing Time">{(["now", "schedule"] as const).map(option => <button key={option} type="button" disabled={disabled} aria-pressed={mode === option} className={`rounded-xl border px-3 py-1.5 text-sm ${mode === option ? "bg-accent" : "hover:bg-accent"}`} onClick={() => onModeChange(option)}>{option === "now" ? "Post Now" : "Schedule"}</button>)}</div>
    {mode === "schedule" ? <PublishTimeField value={value} disabled={disabled} onChange={onChange} /> : null}
  </div>;
}
