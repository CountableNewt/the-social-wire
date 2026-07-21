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
}: {
  environment: EnvironmentName
  path: string
  label: string
}) {
  const { session } = useOperationsAuth()
  const queryClient = useQueryClient()
  const [auditNote, setAuditNote] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const mutation = useMutation({
    mutationFn: () =>
      operationsRequest(session, path, {
        method: "POST",
        body: JSON.stringify({ auditNote, environmentConfirmation: confirmation || undefined }),
      }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["operations-overview", environment] }),
  })
  const allowed = auditNote.trim().length >= 8 && (environment !== "production" || confirmation === "PRODUCTION")
  return (
    <AlertDialog>
      <AlertDialogTrigger render={<Button variant="outline" size="sm" />}>{label}</AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="text-sm font-semibold">{label}?</AlertDialogTitle>
          <AlertDialogDescription className="mt-2 text-xs text-muted-foreground">
            This operator action is immutable in the audit history.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-3 py-3">
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
          <AlertDialogClose
            render={<Button disabled={!allowed || mutation.isPending} onClick={() => mutation.mutate()} />}
          >
            {mutation.isPending ? "Working…" : label}
          </AlertDialogClose>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
