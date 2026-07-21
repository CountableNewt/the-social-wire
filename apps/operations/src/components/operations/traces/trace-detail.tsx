import { OperationsSection } from "@/components/operations/operations-section"
import { Progress } from "@/components/ui/progress"
import type { Span } from "@/lib/operations-types"

export function TraceDetail({ span }: { span?: Span }) {
  if (!span) return <p className="text-xs text-muted-foreground">No trace found.</p>
  const names = [
    "gateway.request",
    "gateway.appview.proxy",
    "appview.auth",
    "appview.cache.lookup",
    "appview.db.query",
    span.name,
  ]
  return (
    <div className="grid gap-3 lg:grid-cols-[1fr_320px]">
      <OperationsSection title={<span className="font-mono">Trace {span.traceId}</span>}>
        <div className="p-4">
          <div className="relative ml-2 border-l pl-5">
            {names.map((name, index) => (
              <div key={`${name}-${index}`} className="relative mb-4 rounded-md border p-3 text-xs">
                <span className="absolute -left-[27px] top-3 size-3 rounded-full border-2 border-primary bg-background" />
                <div className="flex justify-between">
                  <span className="font-mono">{name}</span>
                  <span className="font-mono text-muted-foreground">
                    {Math.round(span.durationMs / (index + 1))} ms
                  </span>
                </div>
                <Progress value={Math.max(8, 100 - index * 14)} className="mt-2" />
              </div>
            ))}
          </div>
        </div>
      </OperationsSection>
      <OperationsSection title="Span Attributes">
        <dl className="divide-y text-[11px]">
          {Object.entries(span.attributes).map(([key, value]) => (
            <div key={key} className="grid grid-cols-2 gap-2 p-3">
              <dt className="font-mono text-muted-foreground">{key}</dt>
              <dd className="break-all font-mono">{value}</dd>
            </div>
          ))}
        </dl>
      </OperationsSection>
    </div>
  )
}
