import Link from "next/link"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { OperationsSection } from "@/components/operations/operations-section"
import { OperatorActionDialog } from "@/components/operations/operator-action-dialog"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { Alert, EnvironmentName, Overview } from "@/lib/operations-types"

const reconnectRules = new Set([
  "jetstream_disconnected",
  "jetstream_connected_idle",
  "jetstream_committed_cursor_stale",
  "jetstream_commit_backlog",
])

function AlertRecoveryAction({
  alert,
  environment,
  reconnectActive,
}: {
  alert: Alert
  environment: EnvironmentName
  reconnectActive: boolean
}) {
  if (reconnectRules.has(alert.rule)) {
    if (reconnectActive) return <Badge tone="warning">Reconnect In Progress</Badge>
    return (
      <OperatorActionDialog
        environment={environment}
        path="/v1/operations/ingestion/reconnect"
        label="Reconnect Jetstream"
        auditNoteRequired={false}
      />
    )
  }
  if (alert.rule === "confirmed_ingestion_gap")
    return <Link href="/gaps" className="text-primary">Investigate Gaps</Link>
  if (alert.rule === "backfill_without_progress" || alert.rule === "terminal_backfill_failure")
    return <Link href="/backfills" className="text-primary">Manage Backfills</Link>
  return null
}

export function AlertsTable({ data, environment }: { data: Overview; environment: EnvironmentName }) {
  const reconnectActive = (data.commands ?? []).some(
    (command) => command.action === "reconnect_jetstream" && ["queued", "running"].includes(command.status),
  )
  return (
    <OperationsSection title={`Operational Alerts (${data.alerts.length})`}>
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={["Severity", "Status", "Rule", "Summary", "Opened", "Delivery", "Runbook", "Recovery / Lifecycle"]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.alerts.map((alert) => {
            const action =
              alert.status === "open" ? "acknowledge" : alert.status === "acknowledged" ? "resolve" : undefined
            return (
              <TableRow key={alert.id}>
                <TableCell>
                  <Badge tone={alert.severity === "critical" ? "danger" : "warning"}>{alert.severity}</Badge>
                </TableCell>
                <TableCell>{alert.status}</TableCell>
                <TableCell className="font-mono">{alert.rule}</TableCell>
                <TableCell>{alert.summary}</TableCell>
                <TableCell>{new Date(alert.openedAt).toLocaleString()}</TableCell>
                <TableCell>
                  {alert.lastDeliveryError ? (
                    <span className="text-destructive">Failed</span>
                  ) : (
                    `${alert.deliveryAttempts} attempt${alert.deliveryAttempts === 1 ? "" : "s"}`
                  )}
                </TableCell>
                <TableCell>
                  <Link href={`/runbooks#${alert.runbookSlug}`} className="text-primary">
                    Open Runbook
                  </Link>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <AlertRecoveryAction alert={alert} environment={environment} reconnectActive={reconnectActive} />
                    {action ? (
                      <OperatorActionDialog
                        environment={environment}
                        path={`/v1/operations/alerts/${alert.id}/${action}`}
                        label={action === "acknowledge" ? "Acknowledge" : "Resolve"}
                      />
                    ) : (
                      <Badge tone="success">Resolved</Badge>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
