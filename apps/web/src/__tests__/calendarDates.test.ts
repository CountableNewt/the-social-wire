import { expect, test } from "bun:test";
import { calendarMonthDays, earliestLocalScheduleValue, localCalendarDate, localDateTimeValue, scheduledInstant } from "@/components/ContentCalendar/calendarDates";

test("projects local calendar dates and rejects invalid or too-soon publish times", () => {
  const now = new Date(2026, 9, 11, 12, 0);
  expect(localCalendarDate(now)).toBe("2026-10-11");
  expect(localDateTimeValue(now)).toBe("2026-10-11T12:00");
  expect(scheduledInstant("2026-10-11T12:15", now.getTime())).toBe(new Date(2026, 9, 11, 12, 15).toISOString());
  expect(scheduledInstant("2026-10-11T12:00", now.getTime())).toBeUndefined();
  expect(scheduledInstant("2026-02-30T12:00", now.getTime())).toBeUndefined();
  expect(scheduledInstant("2026-10-11T12:01Z", now.getTime())).toBeUndefined();
  expect(scheduledInstant("", now.getTime())).toBeUndefined();
});
test("builds a stable six-week month calendar without UTC date shifts", () => {
  const days = calendarMonthDays(new Date(2026, 1, 15));
  expect(days).toHaveLength(42);
  expect(days[0].getDay()).toBe(0);
  expect(localCalendarDate(days[0])).toBe("2026-02-01");
  expect(days.filter(day => day.getMonth() === 1)).toHaveLength(28);
  expect(localCalendarDate(days[41])).toBe("2026-03-14");
});

test("rounds up to the next quarter hour and requires off-grid schedules to select a new time", () => {
  const now = new Date(2099, 0, 1, 12, 0, 39).getTime();
  expect(earliestLocalScheduleValue(now)).toBe("2099-01-01T12:15");
  expect(scheduledInstant("2099-01-01T12:01", now)).toBeUndefined();
  const existing = new Date(2099, 0, 1, 12, 12, 45).toISOString();
  expect(scheduledInstant("2099-01-01T12:12", now, existing)).toBeUndefined();
  expect(scheduledInstant("2099-01-01T12:15", now, existing)).toBe(new Date(2099, 0, 1, 12, 15).toISOString());
});


test("only quarter-hour choices produce a schedulable instant", () => {
  const now = new Date(2099, 0, 1, 11).getTime();
  for (const minute of ["00", "15", "30", "45"]) expect(scheduledInstant(`2099-01-01T12:${minute}`, now)).toBeTruthy();
  for (const minute of ["01", "14", "16", "29", "44", "59"]) expect(scheduledInstant(`2099-01-01T12:${minute}`, now)).toBeUndefined();
  const aligned = new Date(2099, 0, 1, 12, 15).toISOString();
  expect(scheduledInstant("2099-01-01T12:15", now, aligned)).toBe(aligned);
  expect(earliestLocalScheduleValue(new Date(2099, 0, 1, 23, 59).getTime())).toBe("2099-01-02T00:00");
});

test("quarter-hour selection preserves DST instants and fractional-hour time zones", () => {
  const modulePath = new URL("../components/ContentCalendar/calendarDates.ts", import.meta.url).pathname;
  const evaluate = (timezone: string, expression: string) => {
    const result = Bun.spawnSync([process.execPath, "--eval", `import { scheduledInstant, earliestLocalScheduleValue } from ${JSON.stringify(modulePath)}; console.log(JSON.stringify(${expression}));`], {
      env: { ...process.env, TZ: timezone },
    });
    expect(result.exitCode).toBe(0);
    return JSON.parse(new TextDecoder().decode(result.stdout));
  };
  expect(evaluate("America/New_York", `[
    scheduledInstant("2027-03-14T02:15", Date.parse("2027-03-01T00:00:00Z")),
    scheduledInstant("2026-11-01T01:30", Date.parse("2026-10-01T00:00:00Z"), "2026-11-01T06:30:00.000Z"),
    earliestLocalScheduleValue(Date.parse("2026-11-01T05:59:00Z"))
  ]`)).toEqual([null, "2026-11-01T06:30:00.000Z", null]);
  expect(evaluate("Asia/Kathmandu", `scheduledInstant("2099-01-01T12:15", Date.parse("2099-01-01T00:00:00Z"))`)).toBe("2099-01-01T06:30:00.000Z");
});
