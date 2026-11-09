import Foundation
import OperationsCore

/// Tracks scheduler progress, not global content coverage or the freshness of every remote feed.
actor ThinAppViewMaintenanceState {
  private struct Progress {
    var succeeded = false
    var failed = false
    var deadline: Date
  }
  private var progress: [ThinAppViewMaintenanceJob: Progress] = [:]
  private let rssEnabled: Bool

  init(rssEnabled: Bool) { self.rssEnabled = rssEnabled }

  func started(_ job: ThinAppViewMaintenanceJob, maximumDuration: TimeInterval, at now: Date = Date()) {
    var value = progress[job] ?? Progress(deadline: now)
    value.deadline = now.addingTimeInterval(maximumDuration)
    progress[job] = value
  }

  func completed(_ job: ThinAppViewMaintenanceJob, succeeded: Bool, nextRunAfter: TimeInterval,
    at now: Date = Date()) {
    progress[job] = Progress(succeeded: succeeded, failed: !succeeded,
      deadline: now.addingTimeInterval(nextRunAfter + 60))
  }

  func probe(at now: Date = Date(), cacheRequired: Bool = false) -> OperationsServiceProbeResult {
    var dependencies = [
      "appview_database": "ready", "worker_role": "coordinator", "worker_mode": "maintenance",
      "ingestion_source": "disabled", "ingestion_transport": "disabled",
      "jetstream_replay": "disabled_by_worker_mode", "pds_reconciliation": "disabled_by_worker_mode",
      "maintenance_scope": "rss_and_retention", "freshness_scope": "maintenance_scheduler",
    ]
    dependencies["maintenance_rss_cache"] = cacheRequired ? "ready" : "not_required"
    var ready = true
    var required: Set<String> = ["appview_database", "maintenance_retention"]
    if cacheRequired { required.insert("maintenance_rss_cache") }
    for job in ThinAppViewMaintenanceJob.allCases {
      let key = "maintenance_\(job.rawValue)"
      if job == .rss, !rssEnabled { dependencies[key] = "disabled"; continue }
      required.insert(key)
      guard let value = progress[job] else {
        dependencies[key] = "starting"; ready = false; continue
      }
      if value.failed { dependencies[key] = "failed"; ready = false }
      else if value.deadline <= now { dependencies[key] = "stalled"; ready = false }
      else if !value.succeeded { dependencies[key] = "starting"; ready = false }
      else { dependencies[key] = "ready" }
    }
    return OperationsServiceProbeResult(
      liveness: .healthy, readiness: ready ? .healthy : .degraded,
      freshness: ready ? .healthy : .degraded, completeness: .unknown,
      dependencyState: dependencies, requiredDependencyKeys: required,
      observedAt: now, validUntil: now.addingTimeInterval(5))
  }
}
