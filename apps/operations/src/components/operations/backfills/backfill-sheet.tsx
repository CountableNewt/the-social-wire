"use client"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Play } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
import { BackfillCollectionSelector } from "@/components/operations/backfills/backfill-collection-selector"
import { BackfillProgress, isBackfillTerminal } from "@/components/operations/backfills/backfill-progress"
import { BackfillReadiness } from "@/components/operations/backfills/backfill-readiness"
import { BackfillSummary } from "@/components/operations/backfills/backfill-summary"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Select } from "@/components/ui/select"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Toast } from "@/components/ui/toast"
import { useOperationsAuth } from "@/lib/auth-context"
import { dryRunBackfill, fetchBackfill, operationsRequest } from "@/lib/operations-api"
import { canQueueBackfill, type BackfillReadinessInput } from "@/lib/operations-policy"
import type { Backfill, BackfillDryRun, EnvironmentName, Gap, Overview } from "@/lib/operations-types"
import { initialBackfillCollections } from "@/lib/backfill-collections"

export function BackfillSheet({
  gap,
  environment,
  open,
  onOpenChange,
}: {
  gap?: Gap
  environment: EnvironmentName
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { session } = useOperationsAuth()
  const queryClient = useQueryClient()
  const [auditNote, setAuditNote] = useState("")
  const [reviewed, setReviewed] = useState(false)
  const [productionConfirmation, setProductionConfirmation] = useState("")
  const [sourceMode, setSourceMode] = useState<BackfillDryRun["sourceMode"]>("jetstream_replay")
  const [collections, setCollections] = useState<string[]>(() => initialBackfillCollections(gap?.collections ?? []))
  const [createdJobId, setCreatedJobId] = useState<string>()
  const [notification, setNotification] = useState<{ title: string; description: string; tone: "success" | "error" }>()
  const request = useMemo<BackfillDryRun>(
    () => ({
      gapId: gap?.id,
      sourceMode,
      startCursor: gap?.startCursor,
      endCursor: gap?.endCursor,
      collections,
      authorDids: [],
      batchSize: 1000,
      rateLimit: 500,
      maxConcurrency: 1,
    }),
    [collections, gap, sourceMode],
  )
  const dryRun = useMutation({ mutationFn: () => dryRunBackfill(session, request) })
  const changeSourceMode = (value: string) => {
    setSourceMode(value as BackfillDryRun["sourceMode"])
    dryRun.reset()
    setReviewed(false)
  }
  const changeCollections = (value: string[]) => {
    setCollections(value)
    dryRun.reset()
    setReviewed(false)
  }
  const create = useMutation({
    mutationFn: () =>
      operationsRequest<Backfill>(session, "/v1/operations/backfills", {
        method: "POST",
        body: JSON.stringify({
          dryRun: request,
          expectedEstimate: dryRun.data?.estimatedCount,
          auditNote: auditNote.trim() || undefined,
          environmentConfirmation: productionConfirmation || undefined,
        }),
      }),
    onSuccess: (created) => {
      queryClient.setQueryData(["operations-backfill", environment, created.id], created)
      setCreatedJobId(created.id)
      setNotification({
        title: "Backfill Initiated",
        description: `${created.id} was queued successfully. Live progress is now shown in this panel.`,
        tone: "success",
      })
      void queryClient.invalidateQueries({ queryKey: ["operations-overview", environment] })
    },
    onError: (error) =>
      setNotification({
        title: "Backfill Not Initiated",
        description: error instanceof Error ? error.message : "The recovery job could not be queued.",
        tone: "error",
      }),
  })
  const job = useQuery({
    queryKey: ["operations-backfill", environment, createdJobId],
    queryFn: () => fetchBackfill(session, createdJobId!),
    enabled: Boolean(createdJobId),
    refetchInterval: (query) => (query.state.data && isBackfillTerminal(query.state.data.status) ? false : 2_000),
  })
  const activeJob = job.data ?? create.data
  useEffect(() => {
    if (!notification) return
    const timer = window.setTimeout(() => setNotification(undefined), 6_000)
    return () => window.clearTimeout(timer)
  }, [notification])
  useEffect(() => {
    if (!job.data) return
    queryClient.setQueryData<Overview>(["operations-overview", environment], (overview) => {
      if (!overview) return overview
      const index = overview.backfills.findIndex((candidate) => candidate.id === job.data.id)
      const backfills =
        index === -1
          ? [job.data, ...overview.backfills]
          : overview.backfills.map((candidate) => (candidate.id === job.data.id ? job.data : candidate))
      return { ...overview, backfills }
    })
  }, [environment, job.data, queryClient])
  const readinessInput: BackfillReadinessInput = {
    collectionScopeSelected: collections.length > 0,
    dryRunComplete: Boolean(dryRun.data),
    dryRunConflictFree: Boolean(dryRun.data && dryRun.data.conflicts.length === 0),
    reviewed,
    environment,
    environmentConfirmation: productionConfirmation,
    pending: create.isPending,
  }
  const canRun = canQueueBackfill(readinessInput)
  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle className="text-sm font-semibold">
              {activeJob ? `Backfill ${activeJob.id}` : "Backfill Gap"}
            </SheetTitle>
            <SheetDescription className="mt-1 text-[10px] text-muted-foreground">
              {activeJob
                ? "Live recovery progress and resumable checkpoint state."
                : "Dry-run-first recovery with resumable checkpoints."}
            </SheetDescription>
          </SheetHeader>
          {activeJob ? (
            <BackfillProgress job={activeJob} refreshing={job.isFetching} />
          ) : (
            <div className="flex-1 overflow-y-auto overscroll-contain p-4">
              <div className="flex items-center justify-between border-b pb-3 text-xs">
                <span>
                  Gap{" "}
                  <Badge tone="danger" className="ml-1">
                    {gap?.status ?? "Open"}
                  </Badge>
                </span>
                <span className="text-[10px] text-muted-foreground">
                  {gap ? new Date(gap.detectedAt).toLocaleString() : "No gap selected"}
                </span>
              </div>
              <section className="mt-4">
                <p className="text-xs font-semibold">Cursor / Time Range (μs)</p>
                <div className="mt-2 rounded-md border bg-muted/25 p-3 font-mono text-[10px]">
                  <p>
                    {gap?.startCursor ?? "—"} .. {gap?.endCursor ?? "—"}
                  </p>
                  <p className="mt-2 text-muted-foreground">
                    Δ{" "}
                    {gap?.startCursor !== undefined && gap.endCursor !== undefined
                      ? (gap.endCursor - gap.startCursor).toLocaleString()
                      : "—"}{" "}
                    μs
                  </p>
                </div>
              </section>
              <section className="mt-4">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-semibold">Dry-Run Summary</p>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => dryRun.mutate()}
                    disabled={!gap || collections.length === 0 || dryRun.isPending}
                  >
                    {dryRun.isPending ? "Estimating…" : dryRun.data ? "Refresh Estimate" : "Run Dry-Run"}
                  </Button>
                </div>
                <dl className="mt-2 divide-y rounded-md border text-[10px]">
                  <BackfillSummary
                    label="Modeled Event Estimate"
                    value={dryRun.data?.estimatedCount.toLocaleString() ?? "Required"}
                  />
                  <BackfillSummary
                    label="Rate-Limit Projection"
                    value={dryRun.data ? `~ ${Math.ceil(dryRun.data.estimatedDurationSeconds / 60)} min` : "—"}
                  />
                  <BackfillSummary
                    label="Estimate Basis"
                    value={
                      dryRun.data
                        ? sourceMode === "jetstream_replay"
                          ? "250 events/s × cursor duration"
                          : "100 records × author × collection"
                        : "—"
                    }
                  />
                  <BackfillSummary
                    label="Existing Conflicts"
                    value={dryRun.data ? (dryRun.data.conflicts.length ? String(dryRun.data.conflicts.length) : "None") : "—"}
                  />
                </dl>
                {dryRun.data?.conflicts.length ? (
                  <Alert variant="warning" className="mt-3">
                    <AlertTriangle className="mb-1 size-3.5" />
                    <AlertTitle>Backfill Not Needed or Already Covered</AlertTitle>
                    <AlertDescription>
                      <ul className="grid gap-1">
                        {dryRun.data.conflicts.map((conflict) => (
                          <li key={conflict}>{conflict}</li>
                        ))}
                      </ul>
                    </AlertDescription>
                  </Alert>
                ) : null}
              </section>
              <section className="mt-4">
                <p className="text-xs font-semibold">Backfill Configuration</p>
                <FieldGroup className="mt-3 grid-cols-[1fr_1.2fr]">
                  <FieldLabel>Source Mode</FieldLabel>
                  <Select
                    ariaLabel="Source Mode"
                    value={sourceMode}
                    onValueChange={changeSourceMode}
                    options={[
                      { value: "jetstream_replay", label: "Jetstream Replay" },
                      { value: "pds_reconciliation", label: "PDS Reconciliation" },
                    ]}
                  />
                  <FieldLabel>Start Time (μs)</FieldLabel>
                  <Input className="font-mono" value={request.startCursor ?? ""} readOnly />
                  <FieldLabel>End Time (μs)</FieldLabel>
                  <Input className="font-mono" value={request.endCursor ?? ""} readOnly />
                  <FieldLabel>Collection Filters</FieldLabel>
                  <BackfillCollectionSelector value={collections} onValueChange={changeCollections} />
                  <FieldLabel>Bounded Batch Size</FieldLabel>
                  <Input value={request.batchSize} readOnly />
                  <FieldLabel>Rate Limit</FieldLabel>
                  <Input value={`≤ ${request.rateLimit} events/s`} readOnly />
                  <FieldLabel>Worker Concurrency</FieldLabel>
                  <Input value={`${request.maxConcurrency} (sequential executor)`} readOnly />
                  <FieldLabel>Snapshot End Cursor</FieldLabel>
                  <Input className="font-mono" value={dryRun.data?.snapshotEndCursor ?? "Dry-run required"} readOnly />
                </FieldGroup>
              </section>
              <Field className="mt-4">
                <FieldLabel htmlFor="audit-note">Operator Audit Note (Optional)</FieldLabel>
                <Textarea
                  id="audit-note"
                  value={auditNote}
                  maxLength={280}
                  onChange={(event) => setAuditNote(event.target.value)}
                  placeholder="Add context for this backfill"
                />
                <FieldDescription className="text-right">{auditNote.length} / 280</FieldDescription>
              </Field>
              <label className="mt-4 flex items-start gap-2 text-[10px]">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={reviewed}
                  onChange={(event) => setReviewed(event.target.checked)}
                />
                <span>I have reviewed the dry-run summary, understand the impact, and want to backfill this gap.</span>
              </label>
              {environment === "production" ? (
                <Field className="mt-3">
                  <FieldLabel htmlFor="production-confirmation">Production Confirmation</FieldLabel>
                  <Input
                    id="production-confirmation"
                    value={productionConfirmation}
                    onChange={(event) => setProductionConfirmation(event.target.value)}
                    placeholder="Type PRODUCTION"
                  />
                  <FieldDescription>A second explicit environment confirmation is required.</FieldDescription>
                </Field>
              ) : null}
              <BackfillReadiness input={readinessInput} />
              <Alert variant="warning" className="mt-4">
                <AlertTriangle className="mb-1 size-3.5" />
                <AlertTitle>Backfills can increase load</AlertTitle>
                <AlertDescription>
                  This action cannot be undone, but the job can be paused or cancelled.
                </AlertDescription>
              </Alert>
              {create.isError ? (
                <Alert variant="destructive" className="mt-3">
                  <AlertTitle>Backfill Not Initiated</AlertTitle>
                  <AlertDescription>{create.error.message}</AlertDescription>
                </Alert>
              ) : null}
            </div>
          )}
          {job.isError && activeJob ? (
            <p role="alert" className="border-t px-4 py-2 text-[10px] text-destructive">
              Live updates are temporarily unavailable. Showing the most recent job state.
            </p>
          ) : null}
          <SheetFooter className="flex items-center justify-end gap-3">
            {activeJob ? (
              <Button onClick={() => onOpenChange(false)}>Close</Button>
            ) : (
              <>
                <Button variant="outline" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <AlertDialog>
                  <AlertDialogTrigger render={<Button variant="destructive" disabled={!canRun} />}>
                    <Play /> Run Backfill
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle className="text-sm font-semibold">Run This Backfill?</AlertDialogTitle>
                      <AlertDialogDescription className="mt-2 text-xs text-muted-foreground">
                        The confirmed dry-run will be queued in{" "}
                        {environment === "production" ? "Production" : "Development"}. This action is recorded in the
                        immutable audit history.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogClose render={<Button variant="outline" />}>Cancel</AlertDialogClose>
                      <AlertDialogClose render={<Button variant="destructive" onClick={() => create.mutate()} />}>
                        Queue Backfill
                      </AlertDialogClose>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </>
            )}
          </SheetFooter>
        </SheetContent>
      </Sheet>
      {notification ? <Toast {...notification} onClose={() => setNotification(undefined)} /> : null}
    </>
  )
}
