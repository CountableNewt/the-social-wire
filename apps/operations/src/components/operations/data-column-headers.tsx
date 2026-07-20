import { TableHead } from "@/components/ui/table"
import { Tooltip } from "@/components/ui/tooltip"

export const columnExplanations = {
  "Collection": "The ATProto collection represented by this row.",
  "Create (eps)": "Create events processed per second for this collection.",
  "Update (eps)": "Update events processed per second for this collection.",
  "Delete (eps)": "Delete events processed per second for this collection.",
  "All Ops (eps)": "Combined create, update, and delete events processed per second.",
  "In-Flight": "Events currently received but not yet committed.",
  "p50 (ms) R→C": "Median time from receiving an event to committing its projection.",
  "p95 (ms) R→C": "95th-percentile time from receiving an event to committing its projection.",
  "Errors (eps)": "Processing errors recorded per second.",
  "Time": "The time when this request began.",
  "Request ID": "The identifier for this individual request span.",
  "Trace ID": "The correlation identifier shared by every span in the request trace.",
  "Route": "The API route or operation handled by the span.",
  "Status": "The current result or lifecycle state for this row.",
  "Total Latency": "Total elapsed time from request start to response completion.",
  "Auth": "The authentication method accepted for the request.",
  "Cache": "Whether the request was served from or missed the projection cache.",
  "DB Time": "Time attributed to database work during the request.",
  "Query": "The recorded database query or operation name.",
  "Duration": "Total elapsed time attributed to this database span.",
  "Schema": "The Postgres schema containing this table.",
  "Table": "The Postgres table represented by this row.",
  "Estimated Records": "Postgres's estimated number of live rows from pg_stat_user_tables.",
  "Rows": "The number of records returned or affected by the request.",
  "Resp Freshness": "The age of the data when the response was produced.",
  "View Trace": "Opens the correlated request trace and its component spans.",
  "Accepted (eps)": "Events accepted for processing per second.",
  "Filtered (eps)": "Events intentionally excluded by collection or operation filters per second.",
  "Failed (eps)": "Events that failed processing per second.",
  "p95 Commit Time (ms)": "95th-percentile time required to commit an accepted event.",
  "Newest-Event Age": "Elapsed time since the newest committed event was originally received.",
  "Lag (s)": "Current delay between receipt and committed projection, in seconds.",
  "Cursor / Time Range (μs)": "The Jetstream cursor interval covered by the detected gap.",
  "Reason": "The signal that caused the gap to be detected.",
  "Detected": "When the system first recorded the gap.",
  "Affected Collections": "The number of ATProto collections included in the gap.",
  "Action": "The recovery or lifecycle action currently available for this row.",
  "Backfill ID": "The stable identifier assigned to this recovery job.",
  "Range (μs)": "The cursor interval the backfill is configured to replay.",
  "Progress": "The percentage of the estimated recovery workload completed.",
  "Processed": "The number of events processed by the recovery job.",
  "Rate": "The configured or observed event processing rate for the job.",
  "Checkpoint (μs)": "The latest durable cursor saved by the recovery job.",
  "Severity": "The operational impact level assigned to the alert.",
  "Rule": "The alerting rule that opened this incident.",
  "Summary": "A concise description of the condition that triggered the alert.",
  "Opened": "When the alert entered the open state.",
  "Delivery": "Webhook delivery attempts and the latest delivery result.",
  "Runbook": "The operational procedure associated with this alert.",
  "Service": "The distributed service reporting this health state.",
  "Instance": "The specific running service instance reporting the heartbeat.",
  "Liveness": "Whether the service process is running and responding.",
  "Readiness": "Whether the service is ready to accept production work.",
  "Freshness": "Whether the service data is within its expected age threshold.",
  "Completeness": "Whether known gaps or missing projections affect the service.",
  "Version": "The deployed build or release version reported by the service.",
  "Heartbeat": "The most recent health heartbeat received from the instance.",
} as const

export type DataColumnLabel = keyof typeof columnExplanations

export function DataColumnHeaders({ labels }: { labels: readonly DataColumnLabel[] }) {
  return labels.map((label) => (
    <TableHead key={label}>
      <Tooltip label={columnExplanations[label]}>
        <span
          aria-label={`${label}: ${columnExplanations[label]}`}
          className="cursor-help decoration-dotted underline-offset-4 hover:underline focus-visible:underline focus-visible:outline-none"
          tabIndex={0}
        >
          {label}
        </span>
      </Tooltip>
    </TableHead>
  ))
}
