import type { Overview } from "@/lib/operations-types"

const now = new Date()
const iso = (secondsAgo = 0) => new Date(now.getTime() - secondsAgo * 1000).toISOString()
export const demoOverview: Overview = {
  services: ["gateway", "appview", "appview-worker", "operations"].map((service, index) => ({ service, environment: "development", instanceId: `dev-${service}-${index + 1}`, liveness: "healthy", readiness: "healthy", freshness: index === 2 ? "degraded" : "healthy", completeness: index === 1 ? "degraded" : "healthy", dependencyState: { database: "ready", internalTrust: "ready" }, version: "sha-8f42c1d", startedAt: iso(184000), heartbeatAt: iso(6 + index) })),
  ingestion: { source: "jetstream", connectionState: "connected", connectedAt: iso(224000), lastReceivedCursor: 1747488467123456, lastReceivedEventAt: iso(2.1), lastReceivedAt: iso(2), lastCommittedCursor: 1747488465023123, lastCommittedEventAt: iso(4.2), lastCommittedAt: iso(4), queueDepth: 128, heartbeatAt: iso(2) },
  gaps: [
    { id: "gap-20250516-001", source: "jetstream", startCursor: 1747487745123000, endCursor: 1747487750123000, reason: "jetstream_disconnect", status: "confirmed", collections: ["site.standard.document", "site.standard.entry", "app.skyreader.feed.subscription", "site.standard.graph.subscription"], detectedAt: iso(74000), updatedAt: iso(70000), discoveredCount: 1982341, processedCount: 0, failedCount: 0, reconciledCount: 0 },
    { id: "gap-20250517-002", source: "jetstream", startCursor: 1747488451121000, endCursor: 1747488455123000, reason: "consumer_restart", status: "suspected", collections: ["site.standard.document", "site.standard.entry"], detectedAt: iso(25000), updatedAt: iso(24000), discoveredCount: 182432, processedCount: 0, failedCount: 0, reconciledCount: 0 },
    { id: "gap-20250515-003", source: "jetstream", startCursor: 1747488129123000, endCursor: 1747488132123000, reason: "commit_stalled", status: "backfilling", collections: ["site.standard.entry"], detectedAt: iso(120000), updatedAt: iso(120), backfillJobId: "bf-20250517-001", discoveredCount: 1248932, processedCount: 774338, failedCount: 12, reconciledCount: 0 },
  ],
  backfills: [
    { id: "bf-20250517-001", gapId: "gap-20250515-003", sourceMode: "jetstream_replay", status: "running", startCursor: 1747488451123000, endCursor: 1747488455123000, checkpointCursor: 1747488454123456, collections: ["site.standard.entry"], authorDids: [], batchSize: 1000, rateLimit: 500, maxConcurrency: 4, estimatedCount: 1982341, processedCount: 1248932, failedCount: 12, reconciledCount: 0, requestedByDid: "did:plc:operator", auditNote: "Recover confirmed disconnect gap", leaseOwner: "worker-dev-03", leaseExpiresAt: iso(-45), createdAt: iso(7200), updatedAt: iso(10) },
    { id: "bf-20250517-002", sourceMode: "jetstream_replay", status: "running", startCursor: 1747488436123000, endCursor: 1747488444123000, checkpointCursor: 1747488439123456, collections: ["site.standard.document"], authorDids: [], batchSize: 1000, rateLimit: 400, maxConcurrency: 4, estimatedCount: 1013420, processedCount: 182442, failedCount: 2, reconciledCount: 0, requestedByDid: "did:plc:operator", auditNote: "Validate suspected gap", leaseOwner: "worker-dev-02", leaseExpiresAt: iso(-40), createdAt: iso(3600), updatedAt: iso(14) },
    { id: "bf-20250516-005", sourceMode: "pds_reconciliation", status: "completed", collections: ["app.skyreader.feed.subscription"], authorDids: ["did:plc:sample"], batchSize: 500, rateLimit: 250, maxConcurrency: 2, estimatedCount: 512991, processedCount: 512991, failedCount: 0, reconciledCount: 512991, requestedByDid: "did:plc:operator", auditNote: "Reconcile outside replay window", createdAt: iso(86400), updatedAt: iso(82000), completedAt: iso(82000) },
  ],
  alerts: [
    { id: "alert-001", rule: "confirmed_unresolved_gap", severity: "critical", status: "open", summary: "A confirmed ingestion gap requires recovery", evidence: { gap_id: "gap-20250516-001", duration_us: "5000000" }, runbookSlug: "confirming-and-scoping-a-gap", openedAt: iso(70000), updatedAt: iso(70000), deliveryAttempts: 1 },
    { id: "alert-002", rule: "commit_cursor_stale", severity: "warning", status: "acknowledged", summary: "Committed cursor freshness exceeded five minutes", evidence: { age_seconds: "322" }, runbookSlug: "live-process-stalled-ingestion", openedAt: iso(9000), updatedAt: iso(8000), acknowledgedByDid: "did:plc:operator", deliveryAttempts: 2 },
  ],
  recentTraces: [
    ["gateway.request", 812, "200", "/v1/appview/bootstrap-stream"], ["appview.request", 286, "200", "/v1/appview/entries"], ["appview.cache.lookup", 95, "200", "/v1/appview/unread-counts"], ["gateway.appview.proxy", 143, "200", "/v1/publications/sidebar"], ["worker.index.commit", 1210, "error", "site.standard.entry"],
  ].map(([name, duration, status, route], index) => ({ id: `span-${index}`, traceId: `e2f3a4b5c6d7890${index}`.padEnd(32, "a"), service: String(name).split(".")[0], name: String(name), startedAt: iso(20 + index * 8), durationMs: Number(duration), status: String(status), attributes: { route: String(route), cache_outcome: index === 2 ? "hit" : "miss", query_name: "overview" }, expiresAt: iso(-604800) })),
  database: {
    databaseSizeBytes: 3_006_477_312,
    activeConnections: 8,
    maxConnections: 15,
    transactionsTotal: 4_826_341,
    estimatedRecords: 9_842_117,
    cacheHitRatio: 0.997,
    statsResetAt: iso(604800),
    topTables: [
      { schema: "public", table: "content_items", estimatedRecords: 6_482_193 },
      { schema: "public", table: "appview_read_marks", estimatedRecords: 1_924_882 },
      { schema: "public", table: "appview_projection_caches", estimatedRecords: 842_117 },
      { schema: "public", table: "operations_trace_spans", estimatedRecords: 592_925 },
    ],
  },
  refreshedAt: iso(),
}
