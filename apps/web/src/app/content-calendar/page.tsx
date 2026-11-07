import { ContentCalendarWorkspace } from "@/components/ContentCalendar/ContentCalendarWorkspace";
import { notFound } from "next/navigation";
import { isPostSchedulingEnabled } from "@/lib/postScheduling";

export default function ContentCalendarPage() {
  if (!isPostSchedulingEnabled()) notFound();
  return <ContentCalendarWorkspace />;
}
