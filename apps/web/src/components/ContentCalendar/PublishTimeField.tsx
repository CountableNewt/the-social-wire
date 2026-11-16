"use client";

import { useEffect, useId, useState } from "react";
import { earliestLocalScheduleValue, scheduledInstant } from "./calendarDates";

export function PublishTimeField({ label = "Publish At", value, disabled, onChange, originalTime }: {
  label?: string;
  value: string;
  disabled: boolean;
  originalTime?: string;
  onChange: (value: string) => void;
}) {
  const id = useId();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(timer);
  }, []);
  const instant = scheduledInstant(value, now, originalTime);
  const minimum = earliestLocalScheduleValue(now);
  const localTime = new Intl.DateTimeFormat(undefined, {
    year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short",
  });
  return <div className="space-y-2">
    <label className="block space-y-1 text-sm">
      <span>Publish At</span>
      <input aria-label={label} aria-describedby={id} type="datetime-local" min={instant && minimum && value < minimum ? value : minimum} value={value}
        disabled={disabled} onChange={event => onChange(event.target.value)}
        className="w-full rounded-xl border bg-background px-3 py-2" />
    </label>
    <p id={id} className="text-xs text-muted-foreground">Browser Time Zone · {localTime.resolvedOptions().timeZone}</p>
    {instant
      ? <p className="text-xs text-muted-foreground">Publishes {localTime.format(new Date(instant))}</p>
      : <p role={value ? "alert" : undefined} className={value ? "text-xs text-destructive" : "text-xs text-muted-foreground"}>
          Choose a Local Time At Least One Minute From Now.
        </p>}
  </div>;
}
