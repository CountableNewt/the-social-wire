import { ExternalLink } from "lucide-react"
import Link from "next/link"
import { GapTable } from "@/components/operations/gaps/gap-table"
import { OperationsSection } from "@/components/operations/operations-section"
import { partitionGapsByBackfillCompletion } from "@/lib/gap-sections"
import type { Backfill, Gap } from "@/lib/operations-types"

export function GapsTable({
  gaps,
  backfills,
  onSelect,
  onInvestigate,
  expanded = false,
}: {
  gaps: Gap[]
  backfills: Backfill[]
  onSelect: (gap: Gap) => void
  onInvestigate: (gap: Gap) => void
  expanded?: boolean
}) {
  const { activeGaps, backfilledGaps } = partitionGapsByBackfillCompletion(gaps, backfills)
  const action = expanded ? undefined : (
    <Link href="/gaps" className="text-[10px] text-primary">
      View All Gaps <ExternalLink className="inline size-3" />
    </Link>
  )

  return (
    <div className="grid gap-3">
      <OperationsSection title={`Active Gaps (${activeGaps.length})`} action={action}>
        <GapTable
          gaps={activeGaps}
          onSelect={onSelect}
          onInvestigate={onInvestigate}
          allowBackfill
          emptyMessage="No active gaps."
        />
      </OperationsSection>
      {expanded ? (
        <OperationsSection title={`Backfilled Gaps (${backfilledGaps.length})`}>
          <GapTable
            gaps={backfilledGaps}
            onSelect={onSelect}
            onInvestigate={onInvestigate}
            allowBackfill={false}
            emptyMessage="No backfilled gaps."
          />
        </OperationsSection>
      ) : null}
    </div>
  )
}
