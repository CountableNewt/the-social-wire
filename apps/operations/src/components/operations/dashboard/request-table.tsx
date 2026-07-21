import { ExternalLink } from "lucide-react"
import Link from "next/link"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { OperationsSection } from "@/components/operations/operations-section"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { Span } from "@/lib/operations-types"

export function RequestTable({ spans }: { spans: Span[] }) {
  return (
    <OperationsSection
      title={
        <span>
          AppView Request{" "}
          <span className="ml-2 text-[9px] font-normal text-muted-foreground">
            Showing recent correlated requests (last 15m)
          </span>
        </span>
      }
      action={
        <Link href="/appview" className="text-[10px] text-primary">
          View all requests <ExternalLink className="inline size-3" />
        </Link>
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Time",
                "Request ID",
                "Trace ID",
                "Route",
                "Status",
                "Total Latency",
                "Auth",
                "Cache",
                "DB Time",
                "Rows",
                "Resp Freshness",
                "View Trace",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {spans.slice(0, 5).map((span) => (
            <TableRow key={span.id}>
              <TableCell className="font-mono">{new Date(span.startedAt).toLocaleTimeString()}</TableCell>
              <TableCell className="font-mono">{span.id}</TableCell>
              <TableCell className="font-mono">{span.traceId.slice(0, 16)}</TableCell>
              <TableCell className="max-w-64 truncate font-mono">{span.attributes.route ?? span.name}</TableCell>
              <TableCell>
                <span className={span.status === "error" ? "text-destructive" : "ops-success"}>
                  {span.status === "error" ? "500" : span.status}
                </span>
              </TableCell>
              <TableCell className="font-mono">{span.durationMs.toLocaleString()} ms</TableCell>
              <TableCell>JWT</TableCell>
              <TableCell>{span.attributes.cache_outcome?.toUpperCase() ?? "MISS"}</TableCell>
              <TableCell className="font-mono">{Math.round(span.durationMs * 0.12)} ms</TableCell>
              <TableCell>{span.name.includes("entries") ? 50 : 4}</TableCell>
              <TableCell>&lt; 1s</TableCell>
              <TableCell>
                <Link href={`/traces/${span.traceId}`} className="text-primary">
                  View Trace <ExternalLink className="inline size-3" />
                </Link>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
