import type { OAuthSession } from "@/lib/auth"
import { authFetch } from "@/lib/auth"
import { operationsEnvironment } from "@/lib/app-environment"
import { demoGapInvestigation, demoOverview } from "@/lib/demo-data"
import type { BackfillDryRun, DryRunResult, GapInvestigation, Overview } from "@/lib/operations-types"

export function gatewayOrigin() {
  return process.env.NEXT_PUBLIC_OPERATIONS_GATEWAY_ORIGIN
    || (operationsEnvironment() === "production" ? "https://api.thesocialwire.app" : "https://api.testing.thesocialwire.app")
}
const requestId = () => crypto.randomUUID()
const randomHex = (bytes: number) => Array.from(crypto.getRandomValues(new Uint8Array(bytes)), (value) => value.toString(16).padStart(2, "0")).join("")
const traceparent = () => `00-${randomHex(16)}-${randomHex(8)}-01`
export async function operationsRequest<T>(session: OAuthSession | null, path: string, init?: RequestInit): Promise<T> {
  if (process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1") {
    await new Promise((resolve) => setTimeout(resolve, 80))
    if (path === "/v1/operations/overview") return demoOverview as T
    if (/^\/v1\/operations\/gaps\/[^/]+\/investigation$/.test(path)) return demoGapInvestigation(path.split("/")[4]!) as T
    if (path === "/v1/operations/backfills/dry-run") return { estimatedCount: 1_982_341, estimatedDurationSeconds: 3965, snapshotEndCursor: 1747487750123000, conflicts: [], unresolvedDeletesWarning: false } as T
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
export const fetchOverview = (session: OAuthSession | null) => operationsRequest<Overview>(session, "/v1/operations/overview")
export const fetchGapInvestigation = (session: OAuthSession | null, gapId: string) => operationsRequest<GapInvestigation>(session, `/v1/operations/gaps/${encodeURIComponent(gapId)}/investigation`)
export const dryRunBackfill = (session: OAuthSession | null, request: BackfillDryRun) => operationsRequest<DryRunResult>(session, "/v1/operations/backfills/dry-run", { method: "POST", body: JSON.stringify(request) })
export class OperationsForbiddenError extends Error { constructor() { super("This DID is not authorized for operations access") } }
