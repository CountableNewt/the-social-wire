import { OperationsSection } from "@/components/operations/operations-section"
import { Badge } from "@/components/ui/badge"
import { boundedNonNegativeInteger, elapsedSeconds } from "@/lib/observability-values"
import type { Overview } from "@/lib/operations-types"

function formatDuration(seconds: number | null) {
  if (seconds === null) return "—"
  const total = Math.floor(seconds)
  const days = Math.floor(total / 86_400)
  const hours = Math.floor((total % 86_400) / 3_600)
  const minutes = Math.floor((total % 3_600) / 60)
  const remainingSeconds = total % 60
  return [days ? `${days}d` : "", hours ? `${hours}h` : "", minutes ? `${minutes}m` : "", `${remainingSeconds}s`]
    .filter(Boolean)
    .join(" ")
}

function formatTimestamp(value?: string) {
  if (!value) return "—"
  const timestamp = new Date(value)
  return Number.isFinite(timestamp.getTime()) ? timestamp.toLocaleString() : "Invalid timestamp"
}

export function LiveStream({ data }: { data: Overview }) {
  const state = data.ingestion
  const receivedCursor = boundedNonNegativeInteger(state?.lastReceivedCursor)
  const committedCursor = boundedNonNegativeInteger(state?.lastCommittedCursor)
  const cursorDelta = receivedCursor !== null && committedCursor !== null ? receivedCursor - committedCursor : null
  const connectionState = state?.connectionState ?? "unknown"
  const connectionTone =
    connectionState === "connected" ? "success" : connectionState === "unknown" ? "neutral" : "danger"
  const metrics = [
    ["Source", state?.source ?? "—"],
    ["Connected Since", formatTimestamp(state?.connectedAt)],
    ["Connection Duration", formatDuration(elapsedSeconds(state?.connectedAt, data.refreshedAt))],
    ["Last Received (μs)", receivedCursor?.toLocaleString() ?? "—"],
    ["Last Committed (μs)", committedCursor?.toLocaleString() ?? "—"],
    ["Cursor Delta", cursorDelta === null ? "—" : cursorDelta < 0 ? "Invalid ordering" : `${cursorDelta.toLocaleString()} μs`],
    ["Last Received Event", formatTimestamp(state?.lastReceivedEventAt)],
    ["Last Committed Event", formatTimestamp(state?.lastCommittedEventAt)],
    ["In-Flight", boundedNonNegativeInteger(state?.queueDepth)?.toLocaleString() ?? "—"],
    ["Reconnect Reason", state?.lastDisconnectReason ?? "—"],
  ]
  return (
    <OperationsSection
      title={
        <span className="flex items-center gap-2">
          Live Stream / Consumer <Badge tone={connectionTone}>● {connectionState}</Badge>
        </span>
      }
    >
      <div className="grid grid-cols-2 divide-x divide-y sm:grid-cols-3 xl:grid-cols-5">
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
