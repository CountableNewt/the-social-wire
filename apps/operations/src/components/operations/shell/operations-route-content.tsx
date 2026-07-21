import { AlertsTable } from "@/components/operations/dashboard/alerts-table"
import { BackfillsTable } from "@/components/operations/backfills/backfills-table"
import { CollectionHealth } from "@/components/operations/dashboard/collection-health"
import { CollectionTable } from "@/components/operations/dashboard/collection-table"
import { DatabaseObservability } from "@/components/operations/dashboard/database-observability"
import { GapsTable } from "@/components/operations/gaps/gaps-table"
import { HealthStrip } from "@/components/operations/dashboard/health-strip"
import { LiveStream } from "@/components/operations/dashboard/live-stream"
import { RequestTable } from "@/components/operations/dashboard/request-table"
import { Runbooks } from "@/components/operations/runbooks"
import { ServiceTable } from "@/components/operations/dashboard/service-table"
import { TraceDetail } from "@/components/operations/traces/trace-detail"
import type { Runbook } from "@/components/operations/shell/operations-view-types"
import type { EnvironmentName, Gap, Overview } from "@/lib/operations-types"

export function OperationsRouteContent({
  current,
  traceId,
  data,
  environment,
  runbooks,
  onSelectGap,
  onInvestigateGap,
}: {
  current: string
  traceId?: string
  data: Overview
  environment: EnvironmentName
  runbooks: Runbook[]
  onSelectGap: (gap: Gap) => void
  onInvestigateGap: (gap: Gap) => void
}) {
  if (current === "ingestion")
    return (
      <div className="grid gap-3">
        <LiveStream data={data} />
        <CollectionTable metricRollups={data.metricRollups ?? []} />
        <GapsTable
          gaps={data.gaps}
          backfills={data.backfills}
          onSelect={onSelectGap}
          onInvestigate={onInvestigateGap}
        />
      </div>
    )
  if (current === "appview")
    return (
      <div className="grid gap-3">
        <RequestTable spans={data.recentTraces} />
        <DatabaseObservability overview={data} />
        <ServiceTable data={data} />
      </div>
    )
  if (current === "gaps")
    return (
      <GapsTable
        gaps={data.gaps}
        backfills={data.backfills}
        onSelect={onSelectGap}
        onInvestigate={onInvestigateGap}
        expanded
      />
    )
  if (current === "backfills") return <BackfillsTable backfills={data.backfills} environment={environment} expanded />
  if (current === "alerts") return <AlertsTable data={data} environment={environment} />
  if (current === "runbooks") return <Runbooks runbooks={runbooks} />
  if (current === "traces")
    return <TraceDetail span={data.recentTraces.find((span) => span.traceId === traceId) ?? data.recentTraces[0]} />
  return (
    <div className="grid gap-3">
      <HealthStrip overview={data} />
      <LiveStream data={data} />
      <CollectionTable metricRollups={data.metricRollups ?? []} />
      <RequestTable spans={data.recentTraces} />
      <DatabaseObservability overview={data} />
      <CollectionHealth metricRollups={data.metricRollups ?? []} />
      <div className="flex w-full flex-col gap-3">
        <GapsTable
          gaps={data.gaps}
          backfills={data.backfills}
          onSelect={onSelectGap}
          onInvestigate={onInvestigateGap}
        />
        <BackfillsTable backfills={data.backfills} environment={environment} />
      </div>
    </div>
  )
}
