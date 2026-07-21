import { DataColumnHeaders } from "@/components/operations/data-column-headers"
import { OperationsSection } from "@/components/operations/operations-section"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import type { Overview } from "@/lib/operations-types"

export function ServiceTable({ data }: { data: Overview }) {
  return (
    <OperationsSection title="Service State">
      <Table>
        <TableHeader>
          <TableRow>
            <DataColumnHeaders
              labels={[
                "Service",
                "Instance",
                "Liveness",
                "Readiness",
                "Freshness",
                "Completeness",
                "Version",
                "Heartbeat",
              ]}
            />
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.services.map((service) => (
            <TableRow key={`${service.service}-${service.instanceId}`}>
              <TableCell className="font-medium">{service.service}</TableCell>
              <TableCell className="font-mono">{service.instanceId}</TableCell>
              <TableCell>{service.liveness}</TableCell>
              <TableCell>{service.readiness}</TableCell>
              <TableCell>{service.freshness}</TableCell>
              <TableCell>{service.completeness}</TableCell>
              <TableCell className="font-mono">{service.version ?? "—"}</TableCell>
              <TableCell>{new Date(service.heartbeatAt).toLocaleTimeString()}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperationsSection>
  )
}
