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
          Recent Sampled Spans{" "}
          <span className="ml-2 text-[9px] font-normal text-muted-foreground">Observed in the last 15 minutes</span>
        </span>
      }
      action={
        <Link href="/appview" className="text-[10px] text-primary">
          View all sampled spans <ExternalLink className="inline size-3" />
        </Link>
      }
    >
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Time",
                "Span ID",
                "Trace ID",
                "Service",
                "Operation",
                "Method",
                "Status",
                "Status Class",
                "Total Latency",
                "Error Type",
                "Environment",
                "View Trace",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {spans.length === 0 ? (
            <TableRow>
              <TableCell colSpan={12} className="py-8 text-center text-muted-foreground">
                No sampled spans were reported in this time range.
              </TableCell>
            </TableRow>
          ) : (
            spans.slice(0, 5).map((span) => (
              <TableRow key={span.id}>
                <TableCell className="font-mono">{new Date(span.startedAt).toLocaleTimeString()}</TableCell>
                <TableCell className="font-mono">{span.id}</TableCell>
                <TableCell className="font-mono">{span.traceId.slice(0, 16)}</TableCell>
                <TableCell>{span.service}</TableCell>
                <TableCell className="max-w-64 truncate font-mono">
                  {span.attributes.route_template ?? span.name}
                </TableCell>
                <TableCell className="font-mono">{span.attributes.method ?? "—"}</TableCell>
                <TableCell>
                  <span className={span.status === "error" ? "text-destructive" : "ops-success"}>{span.status}</span>
                </TableCell>
                <TableCell className="font-mono">{span.attributes.status_class ?? "—"}</TableCell>
                <TableCell className="font-mono">{span.durationMs.toLocaleString()} ms</TableCell>
                <TableCell className="font-mono">{span.attributes.error_type ?? "—"}</TableCell>
                <TableCell>{span.attributes.environment ?? "—"}</TableCell>
                <TableCell>
                  <Link href={`/traces/${span.traceId}`} className="text-primary">
                    View Trace <ExternalLink className="inline size-3" />
                  </Link>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
