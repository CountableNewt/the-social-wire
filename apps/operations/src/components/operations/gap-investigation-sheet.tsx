"use client"

import { useQuery } from "@tanstack/react-query"
import { AlertTriangle, ArrowRight, CheckCircle2, ExternalLink, Search, XCircle } from "lucide-react"
import Link from "next/link"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { useOperationsAuth } from "@/lib/auth-context"
import { fetchGapInvestigation } from "@/lib/operations-api"
import type { Gap, GapCauseAssessment, GapInvestigation, GapInvestigationEvidence } from "@/lib/operations-types"

export function GapInvestigationSheet({ gap, open, onOpenChange, onBackfill }: { gap?: Gap; open: boolean; onOpenChange: (open: boolean) => void; onBackfill: (gap: Gap) => void }) {
  const auth = useOperationsAuth()
  const investigation = useQuery({
    queryKey: ["gap-investigation", gap?.id],
    queryFn: () => fetchGapInvestigation(auth.session, gap!.id),
    enabled: open && Boolean(gap),
  })

  return <Sheet open={open} onOpenChange={onOpenChange}><SheetContent className="w-[min(96vw,560px)]"><SheetHeader><div className="flex items-center gap-2"><Search className="size-4 text-primary" /><SheetTitle className="text-sm font-semibold">Investigate Gap</SheetTitle></div><SheetDescription className="mt-1 font-mono text-[10px]">{gap?.id}</SheetDescription></SheetHeader><div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-4">{investigation.isLoading ? <InvestigationSkeleton /> : investigation.data ? <GapInvestigationContent investigation={investigation.data} /> : <div role="alert" className="rounded-md border border-destructive/30 bg-danger-surface p-3 text-xs text-destructive"><XCircle className="mb-1 size-4" />{investigation.error instanceof Error ? investigation.error.message : "Investigation evidence could not be loaded."}</div>}</div>{investigation.data ? <SheetFooter className="flex items-center justify-between gap-3"><p className="text-[10px] text-muted-foreground">Review evidence before choosing recovery scope.</p><Button onClick={() => onBackfill(investigation.data.gap)}>Backfill This Gap <ArrowRight /></Button></SheetFooter> : null}</SheetContent></Sheet>
}

export function GapInvestigationContent({ investigation }: { investigation: GapInvestigation }) {
  const evidenceIds = new Set(investigation.assessment.evidenceIds)
  return <div className="grid gap-4"><section aria-labelledby="cause-assessment-title" className="rounded-md border bg-card p-3"><div className="flex flex-wrap items-center gap-2"><h2 id="cause-assessment-title" className="text-xs font-semibold">Likely Trigger</h2><ConfidenceBadge assessment={investigation.assessment} /></div><p className="mt-2 text-sm font-medium">{investigation.assessment.title}</p><p className="mt-1 text-[11px] leading-5 text-muted-foreground">{investigation.assessment.summary}</p></section><section aria-labelledby="evidence-title"><div className="mb-2 flex items-end justify-between gap-3"><div><h2 id="evidence-title" className="text-xs font-semibold">Evidence Timeline</h2><p className="mt-0.5 text-[9px] text-muted-foreground">{formatDate(investigation.windowStart)} – {formatDate(investigation.windowEnd)}</p></div><Badge>{investigation.evidence.length} Signals</Badge></div><ol className="relative ml-2 border-l">{investigation.evidence.map((item) => <EvidenceRow key={item.id} evidence={item} supportsAssessment={evidenceIds.has(item.id)} />)}</ol></section><section aria-labelledby="limitations-title" className="rounded-md border border-warning/30 bg-warning-surface p-3"><div className="flex items-center gap-2 text-warning"><AlertTriangle className="size-3.5" /><h2 id="limitations-title" className="text-xs font-semibold">What This Does Not Prove</h2></div><ul className="mt-2 grid gap-1.5 text-[10px] leading-4 text-warning/90">{investigation.assessment.limitations.map((limitation) => <li key={limitation}>• {limitation}</li>)}</ul></section><section aria-labelledby="next-checks-title"><h2 id="next-checks-title" className="text-xs font-semibold">Next Checks</h2><ul className="mt-2 grid gap-2">{investigation.recommendedActions.map((action) => <li key={action} className="flex gap-2 text-[10px] leading-4 text-muted-foreground"><CheckCircle2 className="mt-0.5 size-3 shrink-0 text-primary" /><span>{action}</span></li>)}</ul></section></div>
}

function ConfidenceBadge({ assessment }: { assessment: GapCauseAssessment }) {
  const tone = assessment.confidence === "high" ? "success" : assessment.confidence === "medium" ? "warning" : assessment.confidence === "low" ? "info" : "neutral"
  return <Badge tone={tone}>{assessment.confidence === "insufficient" ? "Insufficient Evidence" : `${titleCase(assessment.confidence)} Confidence`}</Badge>
}

function EvidenceRow({ evidence, supportsAssessment }: { evidence: GapInvestigationEvidence; supportsAssessment: boolean }) {
  return <li className="relative pb-3 pl-5 last:pb-0"><span className={`absolute -left-[5px] top-1.5 size-2 rounded-full border ${supportsAssessment ? "border-primary bg-primary" : "border-muted-foreground/40 bg-background"}`} /><article className={`rounded-md border p-2.5 ${supportsAssessment ? "border-primary/30 bg-primary/[0.03]" : "bg-card"}`}><div className="flex flex-wrap items-center justify-between gap-2"><div className="flex items-center gap-1.5"><Badge tone={supportsAssessment ? "info" : "neutral"}>{titleCase(evidence.kind)}</Badge>{supportsAssessment ? <span className="text-[9px] font-medium text-primary">Supports Assessment</span> : null}</div><time className="font-mono text-[9px] text-muted-foreground">{formatDate(evidence.occurredAt)}</time></div><h3 className="mt-2 text-[11px] font-medium">{evidence.title}</h3><p className="mt-0.5 text-[10px] leading-4 text-muted-foreground">{evidence.detail}</p><div className="mt-2 flex flex-wrap items-center gap-1.5"><span className="font-mono text-[9px] text-muted-foreground">{evidence.service}</span>{evidence.traceId ? <Link href={`/traces/${evidence.traceId}`} className="inline-flex items-center gap-1 text-[9px] text-primary">Open Trace <ExternalLink className="size-2.5" /></Link> : null}</div></article></li>
}

function InvestigationSkeleton() {
  return <div className="grid gap-4"><Skeleton className="h-28" /><Skeleton className="h-16" /><Skeleton className="h-24" /><Skeleton className="h-20" /></div>
}

function formatDate(value: string) {
  return new Date(value).toLocaleString()
}

function titleCase(value: string) {
  return value.split(/[_\s]+/).map((part) => part ? `${part[0]!.toUpperCase()}${part.slice(1)}` : "").join(" ")
}
