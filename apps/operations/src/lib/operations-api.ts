import type { OAuthSession } from "@/lib/auth"
import { authFetch } from "@/lib/auth"
import { operationsEnvironment } from "@/lib/app-environment"
import { demoGapInvestigation, demoMetricRollups, demoOverview } from "@/lib/demo-data"
import type {
  Backfill,
  BackfillDryRun,
  DryRunResult,
  GapInvestigation,
  Overview,
  OperationsCommand,
  TraceListResponse,
} from "@/lib/operations-types"

let demoCreatedBackfill: Backfill | undefined
let demoReconnectCommand: OperationsCommand | undefined

export function gatewayOrigin() {
  return (
    process.env.NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN ||
    (operationsEnvironment() === "production"
      ? "https://api.thesocialwire.app"
      : "https://api.testing.thesocialwire.app")
  )
}
const requestId = () => crypto.randomUUID()
const randomHex = (bytes: number) =>
  Array.from(crypto.getRandomValues(new Uint8Array(bytes)), (value) => value.toString(16).padStart(2, "0")).join("")
const traceparent = () => `00-${randomHex(16)}-${randomHex(8)}-01`
export async function operationsRequest<T>(session: OAuthSession | null, path: string, init?: RequestInit): Promise<T> {
  if (process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1") {
    await new Promise((resolve) => setTimeout(resolve, 80))
    if (path === "/v1/operations/overview")
      return {
        ...demoOverview,
        commands: demoReconnectCommand
          ? [evolveDemoReconnect(demoReconnectCommand), ...(demoOverview.commands ?? [])]
          : demoOverview.commands,
        backfills: demoCreatedBackfill
          ? [evolveDemoBackfill(demoCreatedBackfill), ...demoOverview.backfills]
          : demoOverview.backfills,
        metricRollups: demoMetricRollups(),
        refreshedAt: new Date().toISOString(),
      } as T
    if (path === "/v1/operations/ingestion/reconnect" && init?.method === "POST") {
      const body = JSON.parse(String(init.body)) as { auditNote: string }
      const createdAt = new Date().toISOString()
      demoReconnectCommand = {
        id: `command-demo-${Date.now()}`,
        action: "reconnect_jetstream",
        status: "queued",
        requestedByDid: "did:plc:demo-operator",
        auditNote: body.auditNote,
        createdAt,
        updatedAt: createdAt,
      }
      return demoReconnectCommand as T
    }
    if (/^\/v1\/operations\/gaps\/[^/]+\/investigation$/.test(path))
      return demoGapInvestigation(path.split("/")[4]!) as T
    if (/^\/v1\/operations\/traces\/[^/]+$/.test(path)) {
      const traceId = decodeURIComponent(path.split("/")[4]!)
      return { spans: demoOverview.recentTraces.filter((span) => span.traceId === traceId) } as T
    }
    if (path === "/v1/operations/backfills/dry-run")
      return {
        estimatedCount: 1_982_341,
        estimatedDurationSeconds: 3965,
        snapshotEndCursor: 1747487750123000,
        conflicts: [],
        unresolvedDeletesWarning: false,
      } as T
    if (path === "/v1/operations/backfills" && init?.method === "POST") {
      const body = JSON.parse(String(init.body)) as {
        dryRun: BackfillDryRun
        expectedEstimate: number
        auditNote?: string
      }
      const createdAt = new Date().toISOString()
      demoCreatedBackfill = {
        id: `bf-demo-${Date.now()}`,
        gapId: body.dryRun.gapId,
        sourceMode: body.dryRun.sourceMode,
        status: "queued",
        startCursor: body.dryRun.startCursor,
        endCursor: body.dryRun.endCursor,
        collections: body.dryRun.collections,
        authorDids: body.dryRun.authorDids,
        batchSize: body.dryRun.batchSize,
        rateLimit: body.dryRun.rateLimit,
        maxConcurrency: body.dryRun.maxConcurrency,
        estimatedCount: body.expectedEstimate,
        processedCount: 0,
        failedCount: 0,
        reconciledCount: 0,
        requestedByDid: "did:plc:demo-operator",
        auditNote: body.auditNote ?? "",
        createdAt,
        updatedAt: createdAt,
      }
      return demoCreatedBackfill as T
    }
    if (/^\/v1\/operations\/backfills\/[^/]+$/.test(path)) {
      const id = decodeURIComponent(path.split("/")[4]!)
      const job =
        demoCreatedBackfill?.id === id
          ? evolveDemoBackfill(demoCreatedBackfill)
          : demoOverview.backfills.find((candidate) => candidate.id === id)
      if (job) return job as T
      throw new Error("Backfill not found")
    }
    return demoOverview as T
  }
  if (!session) throw new Error("Operator authentication is required")
  const headers = new Headers(init?.headers)
  headers.set("Accept", "application/json")
  headers.set("X-Request-ID", requestId())
  headers.set("traceparent", traceparent())
  if (init?.body) headers.set("Content-Type", "application/json")
  const response = await authFetch(session, `${gatewayOrigin()}${path}`, { ...init, headers })
  if (response.status === 403) throw new OperationsForbiddenError()
  if (!response.ok) throw new Error(`Operations request failed (${response.status})`)
  return response.json() as Promise<T>
}
export const fetchOverview = (session: OAuthSession | null) =>
  operationsRequest<Overview>(session, "/v1/operations/overview")
export const fetchBackfill = (session: OAuthSession | null, backfillId: string) =>
  operationsRequest<Backfill>(session, `/v1/operations/backfills/${encodeURIComponent(backfillId)}`)
export const fetchGapInvestigation = (session: OAuthSession | null, gapId: string) =>
  operationsRequest<GapInvestigation>(session, `/v1/operations/gaps/${encodeURIComponent(gapId)}/investigation`)
export const fetchTraceSpans = (session: OAuthSession | null, traceId: string) =>
  operationsRequest<TraceListResponse>(session, `/v1/operations/traces/${encodeURIComponent(traceId)}`)
export const dryRunBackfill = (session: OAuthSession | null, request: BackfillDryRun) =>
  operationsRequest<DryRunResult>(session, "/v1/operations/backfills/dry-run", {
    method: "POST",
    body: JSON.stringify(request),
  })
export class OperationsForbiddenError extends Error {
  constructor() {
    super("This DID is not authorized for operations access")
  }
}

function evolveDemoBackfill(job: Backfill): Backfill {
  const elapsedSeconds = Math.max(0, (Date.now() - new Date(job.createdAt).getTime()) / 1000)
  if (elapsedSeconds < 2) return job
  const processedCount = Math.min(
    job.estimatedCount,
    Math.floor((elapsedSeconds - 2) * job.rateLimit),
  )
  const completed = processedCount >= job.estimatedCount
  const progress = job.estimatedCount > 0 ? processedCount / job.estimatedCount : 0
  return {
    ...job,
    status: completed ? "completed" : "running",
    processedCount,
    checkpointCursor:
      job.startCursor !== undefined && job.endCursor !== undefined
        ? Math.round(job.startCursor + (job.endCursor - job.startCursor) * progress)
        : undefined,
    leaseOwner: completed ? undefined : "worker-demo-01",
    leaseExpiresAt: completed ? undefined : new Date(Date.now() + 30_000).toISOString(),
    updatedAt: new Date().toISOString(),
    completedAt: completed ? new Date().toISOString() : undefined,
  }
}

function evolveDemoReconnect(command: OperationsCommand): OperationsCommand {
  const elapsedSeconds = Math.max(0, (Date.now() - new Date(command.createdAt).getTime()) / 1000)
  if (elapsedSeconds < 1) return command
  if (elapsedSeconds < 3)
    return { ...command, status: "running", claimedBy: "worker-demo-01", updatedAt: new Date().toISOString() }
  return {
    ...command,
    status: "completed",
    claimedBy: "worker-demo-01",
    updatedAt: new Date().toISOString(),
    completedAt: new Date().toISOString(),
  }
}
