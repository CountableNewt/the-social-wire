import { OperationsSection } from "@/components/operations/operations-section"
import { Badge } from "@/components/ui/badge"
import type { Overview } from "@/lib/operations-types"

export function LiveStream({ data }: { data: Overview }) {
  const state = data.ingestion
  const delta = (state?.lastReceivedCursor ?? 0) - (state?.lastCommittedCursor ?? 0)
  const metrics = [
    ["Jetstream Instance", "dev-js-03"],
    ["Connection Duration", "2d 14h 32m 11s"],
    ["Replay Cursor", String((state?.lastCommittedCursor ?? 0) - 5_000_000)],
    ["Last Received (μs)", String(state?.lastReceivedCursor ?? "—")],
    ["Last Committed (μs)", String(state?.lastCommittedCursor ?? "—")],
    ["Cursor Delta", `${delta.toLocaleString()} μs`],
    ["R→C p50 / p95", "410 ms / 1.82 s"],
    ["In-Flight", String(state?.queueDepth ?? 0)],
    ["Reconnect Reason", state?.lastDisconnectReason ?? "—"],
  ]
  return (
    <OperationsSection
      title={
        <span className="flex items-center gap-2">
          Live Stream / Consumers <Badge tone="success">● Connected</Badge>
        </span>
      }
    >
      <div className="grid grid-cols-2 divide-x divide-y sm:grid-cols-3 xl:grid-cols-9 xl:divide-y-0">
        {metrics.map(([label, value]) => (
          <div key={label} className="min-w-0 p-3">
            <p className="text-[9px] text-muted-foreground">{label}</p>
            <p className="mt-1 truncate font-mono text-[10px]">{value}</p>
          </div>
        ))}
      </div>
    </OperationsSection>
  )
}
