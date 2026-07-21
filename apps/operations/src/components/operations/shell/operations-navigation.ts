import { Bell, BookOpenText, LayoutDashboard, RefreshCw, Server, Settings2, Waypoints } from "lucide-react"

export const operationsNav = [
  ["overview", "Overview", LayoutDashboard],
  ["ingestion", "Ingestion", Settings2],
  ["appview", "AppView", Server],
  ["gaps", "Gaps", Waypoints],
  ["backfills", "Backfills", RefreshCw],
  ["alerts", "Alerts", Bell],
  ["runbooks", "Runbooks", BookOpenText],
] as const
