import type { OAuthSession } from "@/lib/auth"
import { authFetch } from "@/lib/auth"
import { demoOverview } from "@/lib/demo-data"
import type { BackfillDryRun, DryRunResult, EnvironmentName, Overview } from "@/lib/operations-types"

export function gatewayOrigin(environment: EnvironmentName) { return environment === "production" ? (process.env.NEXT_PUBLIC_OPERATIONS_PROD_GATEWAY_ORIGIN || "https://api.thesocialwire.app") : (process.env.NEXT_PUBLIC_OPERATIONS_DEV_GATEWAY_ORIGIN || "https://api.testing.thesocialwire.app") }
const requestId = () => crypto.randomUUID()
const randomHex = (bytes: number) => Array.from(crypto.getRandomValues(new Uint8Array(bytes)), (value) => value.toString(16).padStart(2, "0")).join("")
const traceparent = () => `00-${randomHex(16)}-${randomHex(8)}-01`
export async function operationsRequest<T>(session: OAuthSession | null, environment: EnvironmentName, path: string, init?: RequestInit): Promise<T> {
  if (process.env.NEXT_PUBLIC_OPERATIONS_DEMO_MODE === "1") {
    await new Promise((resolve) => setTimeout(resolve, 80))
    if (path === "/v1/operations/overview") return demoOverview as T
    if (path === "/v1/operations/backfills/dry-run") return { estimatedCount: 1_982_341, estimatedDurationSeconds: 3965, snapshotEndCursor: 1747487750123000, conflicts: [], unresolvedDeletesWarning: false } as T
    return demoOverview as T
  }
  if (!session) throw new Error("Operator authentication is required")
  const headers = new Headers(init?.headers)
  headers.set("Accept", "application/json")
  headers.set("X-Request-ID", requestId())
  headers.set("traceparent", traceparent())
  if (init?.body) headers.set("Content-Type", "application/json")
  const response = await authFetch(session, `${gatewayOrigin(environment)}${path}`, { ...init, headers })
  if (response.status === 403) throw new OperationsForbiddenError()
  if (!response.ok) throw new Error(`Operations request failed (${response.status})`)
  return response.json() as Promise<T>
}
export const fetchOverview = (session: OAuthSession | null, environment: EnvironmentName) => operationsRequest<Overview>(session, environment, "/v1/operations/overview")
export const dryRunBackfill = (session: OAuthSession | null, environment: EnvironmentName, request: BackfillDryRun) => operationsRequest<DryRunResult>(session, environment, "/v1/operations/backfills/dry-run", { method: "POST", body: JSON.stringify(request) })
export class OperationsForbiddenError extends Error { constructor() { super("This DID is not authorized for operations access") } }
