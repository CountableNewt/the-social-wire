import { ExternalLink } from "lucide-react"
import Link from "next/link"
import { BackfillRow } from "@/components/operations/backfills/backfill-row"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { OperationsSection } from "@/components/operations/operations-section"
import { Table, TableBody, TableHeader, TableRow } from "@/components/ui/table"
import type { Backfill, EnvironmentName } from "@/lib/operations-types"

export function BackfillsTable({
  backfills,
  environment,
  expanded,
}: {
  backfills: Backfill[]
  environment: EnvironmentName
  expanded?: boolean
}) {
  const action = expanded ? undefined : (
    <Link href="/backfills" className="text-[10px] text-primary">
      View All Backfills <ExternalLink className="inline size-3" />
    </Link>
  )

  return (
    <OperationsSection title={`Active Backfills (${backfills.length})`} action={action}>
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Backfill ID",
                "Status",
                "Collection",
                "Range (μs)",
                "Progress",
                "Processed",
                "Rate",
                "Checkpoint (μs)",
                "Action",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {backfills.map((job) => (
            <BackfillRow key={job.id} job={job} environment={environment} />
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
