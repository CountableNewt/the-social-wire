import AsyncHTTPClient
import Foundation
import Logging

public actor IngressRunner {
  let config: IngressConfiguration
  let database: IngressDatabase
  let health: IngressHealth
  let logger = Logger(label: "socialwire.ingress")
  var tracked: Set<String> = []
  var lastSequence: Int64 = 0
  var lastProgress = ContinuousClock.now
  var lastDurableProgressAt = Date()
  var lastSealed: Int64 = 0
  var pendingLive: [IngressEvent] = []
  var flushingLive = false
  var liveFailure: (any Error)?
  var liveIncidentReset = false
  var budget: ReplayBudget?
  var transport: ArchiveTransport?

  public init(configuration: IngressConfiguration, database: IngressDatabase, health: IngressHealth)
  {
    config = configuration
    self.database = database
    self.health = health
  }
  public func run() async throws {
    try await database.ping()
    await health.database(true)
    // Validate immutable identity before contending for a lease.
    _ = try await database.checkpoint()
    let owner = UUID().uuidString
    var lease: IngressLease?
    while !Task.isCancelled && lease == nil {
      do { lease = try await database.acquire(owner: owner) } catch IngressError.leaseUnavailable {
        try await Task.sleep(for: .seconds(1))
      }
    }
    try Task.checkCancellation()
    guard let lease else { throw IngressError.leaseUnavailable }
    await health.lease(true)
    do {
      try await database.enablePoisonProtection(lease)
      tracked = try await database.trackedDIDs()
      let checkpoint = try await database.checkpoint()
      guard checkpoint?.sequence != nil || config.bootstrapAfter != nil else {
        throw IngressError.invalidConfiguration(
          "JETSTREAM_BOOTSTRAP_AFTER_SEQ is required for a new source generation")
      }
      lastSequence = checkpoint?.sequence ?? 0
      lastDurableProgressAt = checkpoint?.lastProgressAt ?? Date()
      let usage = try await database.usage()
      try await database.pruneReplayUsage()
      let budget = ReplayBudget(
        incidentLimit: config.incidentLimit, dailyLimit: config.dailyLimit,
        incident: checkpoint?.replayBytes ?? usage.reduce(Int64(0), { $0 + $1.1 }), buckets: usage)
      self.budget = budget
      let client = HTTPClient(
        eventLoopGroupProvider: .singleton, configuration: .init(redirectConfiguration: .disallow))
      let transport = ArchiveTransport(
        config: config, client: client, budget: budget, checkpoint: checkpoint,
        recordUsage: database.recordBytes)
      self.transport = transport
      do {
        try await withThrowingTaskGroup(of: Void.self) { group in
          group.addTask { [self] in
            try await subscriptionLoop(lease: lease, transport: transport, budget: budget)
          }
          group.addTask { [self] in
            while !Task.isCancelled {
              try await Task.sleep(for: .milliseconds(Int64(config.leaseTTL / 3 * 1000)))
              try await database.renew(lease)
              try await database.ping()
            }
          }
          group.addTask { [self] in
            while !Task.isCancelled {
              try await Task.sleep(for: .milliseconds(Int64(config.trackedDIDRefresh * 1000)))
              do {
                let refreshed = try await database.trackedDIDs()
                await replaceTracked(refreshed)
                try await database.prunePoison()
              } catch { logger.warning("Tracked repository refresh failed") }
            }
          }
          group.addTask { [self] in
            while !Task.isCancelled {
              try await Task.sleep(for: .seconds(3600))
              try await monitorNoProgress(transport: transport)
            }
          }
          defer { group.cancelAll() }
          _ = try await group.next()
        }
        try await client.shutdown()
      } catch {
        try? await client.shutdown()
        throw error
      }
      try await database.release(lease)
    } catch {
      try? await database.release(lease)
      await health.lease(false)
      await health.stream(false)
      await health.database(false)
      throw IngressDatabase.transactionCause(error)
    }
  }
  private func replaceTracked(_ value: Set<String>) { tracked = value }
  private func subscriptionLoop(
    lease: IngressLease, transport: ArchiveTransport, budget: ReplayBudget
  ) async throws {
    var delay = config.backoffMin
    while !Task.isCancelled {
      let before = lastSequence
      do {
        try await replayAndTail(lease: lease, transport: transport, budget: budget)
      } catch {
        if Task.isCancelled { throw CancellationError() }
        await health.stream(false)
        let category: String
        switch IngressDatabase.transactionCause(error) {
        case IngressError.incidentBudget, IngressError.dailyBudget: category = "replay_budget"
        case IngressError.sourceDiscontinuity: category = "source_discontinuity"
        case IngressError.provider(let message)
        where message.lowercased().contains("consumertooslow"): category = "consumer_too_slow"
        default: category = "transport_error"
        }
        let incidentProgress = await transport.progress(
          state: "failed", after: lastSequence, sealed: lastSealed)
        try await database.incident(
          category: category, sequence: lastSequence, progress: incidentProgress)
        // Keep diagnostics bounded and avoid database/provider credential-bearing error descriptions.
        await health.failed("Jetstream subscription stopped; resuming from durable checkpoint")
        let cause = IngressDatabase.transactionCause(error)
        if case IngressError.sourceDiscontinuity = cause {
          try await database.blockDiscontinuity(lease)
          await health.paused(true)
          while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
        }
        if case IngressError.incidentBudget = error {
          await health.paused(true)
          let progress = await transport.progress(
            state: "paused_budget", after: lastSequence, sealed: 0)
          try await database.updateProgress(progress, lease: lease)
          while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
        }
        if case IngressError.dailyBudget = error {
          await health.paused(true)
          let wait = await budget.dailyWait()
          try await Task.sleep(
            for: .milliseconds(Int64(max(wait, config.replayBudgetPause) * 1000)))
          await health.paused(false)
          continue
        }
        // Lease and SQL errors also restart the subscription from committed state.
        if lastSequence > before { delay = config.backoffMin }
        try await Task.sleep(for: .milliseconds(Int64(Double.random(in: delay / 4...delay) * 1000)))
        delay = min(config.backoffMax, delay * 2)
      }
    }
  }
  private func replayAndTail(lease: IngressLease, transport: ArchiveTransport, budget: ReplayBudget)
    async throws
  {
    let checkpoint = try await database.checkpoint()
    lastSequence = checkpoint?.sequence ?? lastSequence
    pendingLive.removeAll()
    flushingLive = false
    var coveredAfter = checkpoint?.sequence.map { max(0, $0 - 1) } ?? config.bootstrapAfter ?? 0
    while !Task.isCancelled {
      let start = coveredAfter
      var after = start
      var sealed: Int64?
      await health.stream(true)
      while true {
        let plan = try await transport.plan(after: after, before: sealed)
        try plan.validate(after: after, sealed: sealed)
        sealed = plan.sealedTipSeq
        lastSealed = plan.sealedTipSeq
        for pageStart in stride(from: 0, to: plan.segments.count, by: config.downloadConcurrency) {
          let segments = Array(
            plan.segments[
              pageStart..<min(plan.segments.count, pageStart + config.downloadConcurrency)])
          let spools = try await withThrowingTaskGroup(of: (Int, ArchiveSpool).self) { group in
            for (index, segment) in segments.enumerated() where segment.mode == "segment" {
              group.addTask { (index, ArchiveSpool(try await transport.segment(segment))) }
            }
            defer { group.cancelAll() }
            var output: [Int: ArchiveSpool] = [:]
            for try await (index, spool) in group { output[index] = spool }
            return output
          }
          // Downloads can overlap; only ordered, fully decoded blocks may enter staging.
          for (index, segment) in segments.enumerated() {
            if let spool = spools[index] {
              let archive = try ArchiveSegment(url: spool.url, checksum: segment.checksum)
              guard archive.minimum == segment.minSeq, archive.maximum == segment.maxSeq else {
                throw IngressError.malformedArchive("Segment bounds do not match plan")
              }
              for block in archive.blocks.indices {
                let events = try archive.decodeBlock(block, from: spool.url)
                try await stageArchive(
                  events, lower: start, upper: plan.sealedTipSeq, lease: lease, transport: transport
                )
              }
            } else {
              for range in segment.blocks ?? [] {
                for block in range.first...range.last {
                  let events = try ArchiveBlockDecoder.decodeCompressed(
                    await transport.block(segment, index: block))
                  try await stageArchive(
                    events, lower: start, upper: plan.sealedTipSeq, lease: lease,
                    transport: transport)
                }
              }
            }
          }
        }
        if plan.plannedThroughSeq >= plan.sealedTipSeq { break }
        after = plan.plannedThroughSeq
      }
      liveIncidentReset = false
      liveFailure = nil
      let sealedTip = sealed ?? 0
      let seam = max(start, sealedTip)
      lastProgress = .now
      let connection = LiveConnection()
      // Dictionary fetch failure degrades to the protocol's uncompressed text mode.
      let dictionary = try? await transport.liveDictionary()
      try Task.checkCancellation()
      do {
        try await withThrowingTaskGroup(of: Void.self) { group in
          group.addTask { [self] in
            try await connection.consume(
              configuration: config, cursor: seam, dictionary: dictionary
            ) { [self] event in
              try await stageLive(
                event, lease: lease, transport: transport, after: start, sealed: sealedTip)
            }
          }
          group.addTask { [self] in
            while !Task.isCancelled {
              try await Task.sleep(for: .milliseconds(100))
              try await flushLive(
                lease: lease, transport: transport, after: start, sealed: sealedTip)
            }
          }
          group.addTask { [self] in
            while !Task.isCancelled {
              try await Task.sleep(for: .seconds(30))
              if await stalled() {
                connection.close()
                throw IngressError.provider("Live subscription made no progress for ten minutes")
              }
            }
          }
          defer {
            group.cancelAll()
            connection.close()
          }
          _ = try await group.next()
        }
      } catch IngressError.cursorTooOld {
        // The completed archive range is an in-process coverage boundary,
        // never a fabricated durable event checkpoint. Close the new gap
        // before retrying the live handoff; a crash resumes from PostgreSQL.
        pendingLive.removeAll()
        guard seam > coveredAfter else {
          throw IngressError.provider("Archive/live handoff made no coverage progress")
        }
        coveredAfter = seam
        continue
      }
    }
  }
  private func monitorNoProgress(transport: ArchiveTransport) async throws {
    guard Date().timeIntervalSince(lastDurableProgressAt) >= 86400 else { return }
    let progress = await transport.progress(
      state: "failed", after: lastSequence, sealed: lastSealed)
    try await database.incident(
      category: "no_progress_24h", sequence: lastSequence, progress: progress)
  }
  private func stalled() -> Bool { ContinuousClock.now - lastProgress >= .seconds(600) }
  private func stageArchive(
    _ events: [IngressEvent], lower: Int64, upper: Int64, lease: IngressLease,
    transport: ArchiveTransport
  ) async throws {
    let matching = events.filter {
      $0.sequence > lower && $0.sequence <= upper && acceptsCollection($0)
    }
    guard !matching.isEmpty else { return }
    for start in stride(from: 0, to: matching.count, by: config.batchSize) {
      let chunk = Array(matching[start..<min(matching.count, start + config.batchSize)])
      let progress = await transport.progress(state: "replaying", after: lower, sealed: upper)
      try await stage(chunk, lease: lease, progress: progress)
    }
  }
  private func stageLive(
    _ event: IngressEvent, lease: IngressLease, transport: ArchiveTransport, after: Int64,
    sealed: Int64
  ) async throws {
    if let liveFailure { throw liveFailure }
    guard event.sequence > lastSequence, acceptsCollection(event) else { return }
    guard pendingLive.count < config.batchSize * 2 else {
      throw IngressError.provider("Live staging queue is full")
    }
    if let previous = pendingLive.last, previous.sequence >= event.sequence { return }
    pendingLive.append(event)
    if pendingLive.count >= config.batchSize {
      try await flushLive(lease: lease, transport: transport, after: after, sealed: sealed)
    }
  }
  private func flushLive(
    lease: IngressLease, transport: ArchiveTransport, after: Int64, sealed: Int64
  ) async throws {
    if let liveFailure { throw liveFailure }
    guard !flushingLive else { return }
    flushingLive = true
    defer { flushingLive = false }
    while !pendingLive.isEmpty {
      let batch = Array(pendingLive.prefix(config.batchSize))
      pendingLive.removeFirst(batch.count)
      let progress = await transport.progress(state: "live", after: after, sealed: sealed)
      do {
        try await stage(batch, lease: lease, progress: progress)
        if !liveIncidentReset {
          await budget?.resetIncident()
          liveIncidentReset = true
          let live = await transport.progress(state: "live", after: after, sealed: sealed)
          try await database.updateProgress(live, lease: lease)
          await health.progress(sequence: lastSequence, replay: live)
        }
      } catch {
        // Actor reentrancy can let the reader enqueue while this transaction is
        // suspended. Latch failure before another flush may checkpoint later rows.
        liveFailure = error
        throw error
      }
    }
  }
  private func acceptsCollection(_ event: IngressEvent) -> Bool {
    guard let collection = event.collection else { return true }
    return config.collections.contains {
      $0 == collection || $0.hasSuffix(".*") && collection.hasPrefix(String($0.dropLast()))
    }
  }
  private func stage(_ events: [IngressEvent], lease: IngressLease, progress: ReplayProgress)
    async throws
  {
    guard let last = events.max(by: { $0.sequence < $1.sequence }) else { return }
    let prepared = events.filter { $0.kind == "commit" || tracked.contains($0.did) }
    try await database.stage(
      prepared, checkpointSequence: last.sequence, eventTime: last.eventTime, lease: lease,
      progress: progress)
    if last.sequence > lastSequence {
      lastProgress = .now
      lastDurableProgressAt = Date()
    }
    lastSequence = max(lastSequence, last.sequence)
    await health.progress(sequence: lastSequence, replay: progress)
  }
}
