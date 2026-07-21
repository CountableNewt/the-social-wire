import { ExternalLink, Route } from "lucide-react"
import { operationsNav } from "@/components/operations/shell/operations-navigation"
import { Button } from "@/components/ui/button"

export function OperationsPageHeading({ current }: { current: string }) {
  const label =
    operationsNav.find(([key]) => key === current)?.[1] ?? (current === "traces" ? "Trace Detail" : "Overview")
  return (
    <div className="mb-3 flex items-center gap-3">
      <h1 className="text-lg font-semibold tracking-tight">{label}</h1>
      {current === "overview" ? (
        <Button variant="link" size="sm">
          <Route /> Open in Trace / Metrics Explorer <ExternalLink />
        </Button>
      ) : null}
    </div>
  )
}
