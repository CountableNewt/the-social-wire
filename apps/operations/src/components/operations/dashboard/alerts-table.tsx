import Link from "next/link"
import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { OperationsSection } from "@/components/operations/operations-section"
import { OperatorActionDialog } from "@/components/operations/operator-action-dialog"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { EnvironmentName, Overview } from "@/lib/operations-types"

export function AlertsTable({ data, environment }: { data: Overview; environment: EnvironmentName }) {
  return (
    <OperationsSection title={`Operational Alerts (${data.alerts.length})`}>
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={["Severity", "Status", "Rule", "Summary", "Opened", "Delivery", "Runbook", "Action"]}
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
                  {action ? (
                    <OperatorActionDialog
                      environment={environment}
                      path={`/v1/operations/alerts/${alert.id}/${action}`}
                      label={action === "acknowledge" ? "Acknowledge" : "Resolve"}
                    />
                  ) : (
                    <Badge tone="success">Resolved</Badge>
                  )}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
