/** Calendar dates are local wall time; publishing timestamps are always UTC instants. */
export function localCalendarDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
export function localDateTimeValue(date: Date): string {
  return `${localCalendarDate(date)}T${String(date.getHours()).padStart(2, "0")}:${String(date.getMinutes()).padStart(2, "0")}`;
}
export function isQuarterHour(date: Date): boolean {
  return date.getUTCMinutes() % 15 === 0 && date.getUTCSeconds() === 0 && date.getUTCMilliseconds() === 0;
}
export function scheduledInstant(value: string, now = Date.now(), originalTime?: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return undefined;
  const original = originalTime ? new Date(originalTime) : undefined;
  // Keep the exact instant when the displayed minute of an existing schedule is unchanged.
  const date = original && localDateTimeValue(original) === value ? original : new Date(value);
  // Reject browser-normalized invalid dates and nonexistent times across DST transitions.
  return Number.isFinite(date.getTime()) && localDateTimeValue(date) === value && isQuarterHour(date) && date.getTime() >= now + 60_000 ? date.toISOString() : undefined;
}
export function earliestLocalScheduleValue(now = Date.now()): string | undefined {
  const minimum = Math.ceil((now + 60_000) / 900_000) * 900_000;
  const value = localDateTimeValue(new Date(minimum));
  // A native minimum cannot distinguish the two occurrences of a repeated hour.
  return new Date(value).getTime() === minimum ? value : undefined;
}

export function calendarMonthDays(month: Date): Date[] {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const start = new Date(first.getFullYear(), first.getMonth(), 1 - first.getDay());
  return Array.from({ length: 42 }, (_, offset) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + offset));
}
