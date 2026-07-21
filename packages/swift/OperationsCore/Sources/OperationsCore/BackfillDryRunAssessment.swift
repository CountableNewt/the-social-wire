import Foundation

enum BackfillDryRunAssessment {
  static func build(
    request: BackfillDryRunRequest,
    gap: IngestionGap?,
    streamState: IngestionStreamState?,
    existingJobs: [BackfillJob]
  ) -> BackfillDryRunResponse {
    var conflicts: [String] = []
    var rangeAlreadyRecovered = false

    if request.gapId != nil {
      guard let gap else {
        return response(request: request, estimatedCount: 0, conflicts: [
          "The selected gap no longer exists. Refresh the Operations console before continuing."
        ])
      }
      if gap.status == .resolved || gap.status == .ignored {
        conflicts.append("The selected gap is already \(gap.status.rawValue) and does not need a backfill.")
      }
      if request.sourceMode == .jetstreamReplay,
        gap.startCursor != request.startCursor || gap.endCursor != request.endCursor
      {
        conflicts.append("The replay range no longer matches the selected gap. Refresh the dry run.")
      }
      let gapCollections = Set(gap.collections)
      if !gapCollections.isEmpty && gapCollections.isDisjoint(with: request.collections) {
        conflicts.append("The selected collections do not intersect the collections observed for this gap.")
      }
    }

    if request.gapId != nil,
      request.sourceMode == .jetstreamReplay,
      let endCursor = request.endCursor,
      let committedCursor = streamState?.lastCommittedCursor,
      committedCursor >= endCursor
    {
      rangeAlreadyRecovered = true
      conflicts.append(
        "Live ingestion has already committed through this cursor range; replaying it would be a no-op."
      )
    }

    if existingJobs.contains(where: { overlaps($0, request: request) }) {
      conflicts.append("An active backfill already covers this recovery scope.")
    }

    return response(
      request: request,
      estimatedCount: rangeAlreadyRecovered ? 0 : modeledEstimate(for: request),
      conflicts: conflicts
    )
  }

  private static func response(
    request: BackfillDryRunRequest,
    estimatedCount: Int,
    conflicts: [String]
  ) -> BackfillDryRunResponse {
    BackfillDryRunResponse(
      estimatedCount: estimatedCount,
      estimatedDurationSeconds: request.rateLimit > 0
        ? Int(ceil(Double(estimatedCount) / Double(request.rateLimit))) : 0,
      snapshotEndCursor: request.endCursor,
      conflicts: conflicts,
      unresolvedDeletesWarning: request.sourceMode == .pdsReconciliation
    )
  }

  private static func modeledEstimate(for request: BackfillDryRunRequest) -> Int {
    switch request.sourceMode {
    case .jetstreamReplay:
      let delta = max(0, (request.endCursor ?? 0) - (request.startCursor ?? 0))
      return min(Int.max / 2, max(0, Int(Double(delta) / 1_000_000 * 250)))
    case .pdsReconciliation:
      let (authors, authorOverflow) = request.authorDids.count.multipliedReportingOverflow(by: 100)
      let (estimate, collectionOverflow) = authors.multipliedReportingOverflow(
        by: max(1, request.collections.count)
      )
      return authorOverflow || collectionOverflow ? Int.max / 2 : estimate
    }
  }

  private static func overlaps(_ job: BackfillJob, request: BackfillDryRunRequest) -> Bool {
    guard [.queued, .running, .paused].contains(job.status),
      job.sourceMode == request.sourceMode,
      !Set(job.collections).isDisjoint(with: request.collections)
    else { return false }

    if let gapId = request.gapId, job.gapId == gapId { return true }

    switch request.sourceMode {
    case .jetstreamReplay:
      guard let requestStart = request.startCursor, let requestEnd = request.endCursor,
        let jobStart = job.startCursor, let jobEnd = job.endCursor
      else { return false }
      return requestStart < jobEnd && jobStart < requestEnd
    case .pdsReconciliation:
      return !Set(job.authorDids).isDisjoint(with: request.authorDids)
    }
  }
}
