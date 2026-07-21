import { CalendarDays, CircleDot, RefreshCw } from "lucide-react"
import { OperatorMenu } from "@/components/operations/operator-menu"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip } from "@/components/ui/tooltip"
import { healthLabel, overallSystemHealth } from "@/lib/observability-values"
import type { EnvironmentName, Overview } from "@/lib/operations-types"

export function OperationsTopBar({
  environment,
  autoRefresh,
  setAutoRefresh,
  refreshedAt,
  onRefresh,
  operator,
  onSignOut,
  overview,
  demo,
}: {
  environment: EnvironmentName
  autoRefresh: boolean
  setAutoRefresh: (value: boolean) => void
  refreshedAt?: string
  onRefresh: () => void
  operator: string
  onSignOut: () => Promise<void>
  overview?: Overview
  demo: boolean
}) {
  const systemHealth = overview ? overallSystemHealth(overview) : "unknown"
  const systemTone =
    systemHealth === "healthy"
      ? "success"
      : systemHealth === "degraded"
        ? "warning"
        : systemHealth === "unhealthy"
          ? "danger"
          : "neutral"
  return (
    <header className="sticky top-0 z-30 flex min-h-[53px] flex-wrap items-center gap-x-4 gap-y-2 border-b bg-background/95 px-3 py-2 backdrop-blur sm:px-4 md:flex-nowrap">
      <div className="flex items-center gap-2">
        <span className="ops-label normal-case tracking-normal">Environment</span>
        <Badge tone={environment === "production" ? "danger" : "warning"}>
          {environment === "production" ? "Production" : "Development"}
        </Badge>
        {demo ? <Badge tone="info">Demo Data</Badge> : null}
      </div>
      <div className="hidden h-7 border-l sm:block" />
      <div className="flex items-center gap-2 text-xs">
        <span>System State</span>
        <Badge tone={systemTone}>
          <CircleDot className="mr-1 size-2.5" /> {healthLabel(systemHealth)}
        </Badge>
      </div>
      <div className="ml-auto flex items-center gap-3">
        <div className="hidden items-center gap-2 lg:flex">
          <span className="ops-label normal-case tracking-normal">Time Range</span>
          <span className="inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs">
            <span>Last 15 minutes</span>
            <CalendarDays />
          </span>
        </div>
        <label className="flex items-center gap-1.5 text-[10px]">
          <span>Auto-refresh</span>
          <button
            type="button"
            role="switch"
            aria-checked={autoRefresh}
            onClick={() => setAutoRefresh(!autoRefresh)}
            className={`relative h-4 w-7 rounded-full border ${autoRefresh ? "bg-primary" : "bg-muted"}`}
          >
            <span
              className={`absolute top-0.5 size-2.5 rounded-full bg-white transition-[left] ${autoRefresh ? "left-3.5" : "left-0.5"}`}
            />
          </button>
        </label>
        <div className="hidden text-right text-[9px] leading-tight text-muted-foreground xl:block">
          <p>Last refreshed</p>
          <p>{refreshedAt ? new Date(refreshedAt).toLocaleString() : "—"}</p>
        </div>
        <Tooltip label="Refresh Now">
          <Button variant="ghost" size="icon" onClick={onRefresh}>
            <RefreshCw />
          </Button>
        </Tooltip>
        <OperatorMenu operator={operator} onSignOut={onSignOut} />
      </div>
    </header>
  )
}
