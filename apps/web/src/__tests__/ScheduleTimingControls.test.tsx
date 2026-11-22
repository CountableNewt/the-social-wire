import { afterEach, expect, mock, test } from "bun:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ScheduleTimingControls } from "@/components/Social/ScheduleTimingControls";

afterEach(cleanup);
test("switches publishing mode explicitly and validates a local future time", () => {
  const mode = mock(() => {}), change = mock(() => {});
  const view = render(<ScheduleTimingControls mode="now" value="" disabled={false} onModeChange={mode} onChange={change} />);
  expect(screen.queryByLabelText("Publish At")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Schedule" }));
  expect(mode).toHaveBeenCalledWith("schedule");
  view.rerender(<ScheduleTimingControls mode="schedule" value="2000-01-01T12:00" disabled={false} onModeChange={mode} onChange={change} />);
  expect(screen.getByRole("alert").textContent).toContain("At Least One Minute From Now");
  expect(screen.getByText(/Browser Time Zone/).textContent).toContain(Intl.DateTimeFormat().resolvedOptions().timeZone);
  fireEvent.change(screen.getByLabelText("Publish At"), { target: { value: "2099-01-01T12:00" } });
  expect(change).toHaveBeenCalledWith("2099-01-01T12:00");
  view.rerender(<ScheduleTimingControls mode="schedule" value="2099-01-01T12:00" disabled={false} onModeChange={mode} onChange={change} />);
  const date = new Date(2099, 0, 1, 12);
  const localTime = new Intl.DateTimeFormat(undefined, {
    year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short",
  }).format(date);
  expect(screen.getByText(`Scheduled For ${localTime}`)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.getByLabelText("Publish At").getAttribute("step")).toBe("900");
  expect(screen.getByText(/Publishing times are approximate/).textContent).toContain("posts may publish after the selected time");
  view.rerender(<ScheduleTimingControls mode="schedule" value="2099-01-01T12:07" disabled={false} onModeChange={mode} onChange={change} />);
  expect(screen.getByRole("alert").textContent).toContain("Quarter-Hour");
});
