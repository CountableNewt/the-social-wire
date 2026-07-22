"use client"

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
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
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { useOperationsAuth } from "@/lib/auth-context"
import { operationsRequest } from "@/lib/operations-api"
import type { EnvironmentName } from "@/lib/operations-types"

export function OperatorActionDialog({
  environment,
  path,
  label,
  auditNoteRequired = true,
}: {
  environment: EnvironmentName
  path: string
  label: string
  auditNoteRequired?: boolean
}) {
  const { session } = useOperationsAuth()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [auditNote, setAuditNote] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const mutation = useMutation({
    mutationFn: () =>
      operationsRequest(session, path, {
        method: "POST",
        body: JSON.stringify({
          ...(auditNoteRequired ? { auditNote } : {}),
          environmentConfirmation: confirmation || undefined,
        }),
      }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["operations-overview", environment] })
      setOpen(false)
    },
  })
  const allowed =
    (!auditNoteRequired || auditNote.trim().length >= 8) &&
    (environment !== "production" || confirmation === "PRODUCTION")
  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (nextOpen) {
          setAuditNote("")
          setConfirmation("")
          mutation.reset()
        }
      }}
    >
      <AlertDialogTrigger render={<Button variant="outline" size="sm" />}>{label}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-sm font-semibold">{label}?</AlertDialogTitle>
          <AlertDialogDescription className="mt-2 text-xs text-muted-foreground">
            {auditNoteRequired
              ? "This operator action is immutable in the audit history."
              : "The current Jetstream connection state and this operator action are recorded automatically."}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-3 py-3">
          {auditNoteRequired ? (
            <Field>
              <FieldLabel htmlFor={`audit-${label}`}>Operator Audit Note</FieldLabel>
              <Textarea
                id={`audit-${label}`}
                value={auditNote}
                maxLength={280}
                onChange={(event) => setAuditNote(event.target.value)}
                placeholder="Explain why this action is required"
              />
              <FieldDescription>{auditNote.length} / 280</FieldDescription>
            </Field>
          ) : null}
          {environment === "production" ? (
            <Field>
              <FieldLabel htmlFor={`confirm-${label}`}>Production Confirmation</FieldLabel>
              <Input
                id={`confirm-${label}`}
                value={confirmation}
                onChange={(event) => setConfirmation(event.target.value)}
                placeholder="Type PRODUCTION"
              />
            </Field>
          ) : null}
          {mutation.isError ? (
            <p role="alert" className="text-xs text-destructive">
              {mutation.error.message}
            </p>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" />}>Cancel</AlertDialogClose>
          <Button disabled={!allowed || mutation.isPending} onClick={() => mutation.mutate()}>
            {mutation.isPending ? "Working…" : label}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
