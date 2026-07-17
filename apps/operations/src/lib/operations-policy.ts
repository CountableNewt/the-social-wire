import type { EnvironmentName, Span } from "@/lib/operations-types"

export function productionConfirmationMatches(environment: EnvironmentName, value: string) {
  return environment !== "production" || value === "PRODUCTION"
}

export function canQueueBackfill(input: { dryRunComplete: boolean; reviewed: boolean; environment: EnvironmentName; environmentConfirmation: string; auditNote: string; pending: boolean }) {
  return input.dryRunComplete && input.reviewed && productionConfirmationMatches(input.environment, input.environmentConfirmation) && input.auditNote.trim().length >= 8 && !input.pending
}

export function filterTraces(spans: Span[], query: string) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return spans
  return spans.filter((span) => [span.traceId, span.service, span.name, ...Object.values(span.attributes)].some((value) => value.toLowerCase().includes(normalized)))
}
