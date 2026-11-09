import AsyncHTTPClient
import Foundation

/// Finite direct-PDS checks, shared by callers without allocating an unbounded task or waiter queue.
public actor AuthoritativeContentVerifier: AuthoritativeContentVerifying {
  private struct Cached {
    let proof: AuthoritativeContentCheck.Proof
    let expiresAt: Date
  }
  private struct Flight {
    let task: Task<Void, Never>
    var waiters: [UUID: CheckedContinuation<Bool, any Error>]
  }
  private let check: AuthoritativeContentCheck
  private let cacheTTL: TimeInterval
  private let maximumConcurrentChecks: Int
  private let maximumWaitersPerEntry: Int
  private var stopped = false
  private var cache: [String: Cached] = [:]
  private var flights: [String: Flight] = [:]

  public init(store: any ThinAppViewStore, indexer: ThinAppViewIndexer,
    httpClient: HTTPClient, plcURL: String) {
    check = AuthoritativeContentCheck(store: store, indexer: indexer,
      transport: RepositorySnapshotPublicHTTPTransport(httpClient: httpClient), plcURL: plcURL)
    cacheTTL = 60
    maximumConcurrentChecks = 4
    maximumWaitersPerEntry = 8
  }

  init(check: AuthoritativeContentCheck, cacheTTL: TimeInterval = 60,
    maximumConcurrentChecks: Int = 4, maximumWaitersPerEntry: Int = 8) {
    self.check = check
    self.cacheTTL = max(0, min(60, cacheTTL))
    self.maximumConcurrentChecks = max(1, min(4, maximumConcurrentChecks))
    self.maximumWaitersPerEntry = max(1, min(8, maximumWaitersPerEntry))
  }

  public func verify(entryURI: String) async throws -> Bool {
    try Task.checkCancellation()
    guard !stopped else { throw CancellationError() }
    let scope = try AuthoritativeContentCheck.Scope(entryURI)
    let id = UUID()
    return try await withTaskCancellationHandler {
      try await withCheckedThrowingContinuation { continuation in
        guard !Task.isCancelled, !stopped else { continuation.resume(throwing: CancellationError()); return }
        if var flight = flights[entryURI] {
          guard flight.waiters.count < maximumWaitersPerEntry else {
            continuation.resume(throwing: AuthoritativeContentVerificationError.capacityExceeded); return
          }
          flight.waiters[id] = continuation
          flights[entryURI] = flight
          return
        }
        guard flights.count < maximumConcurrentChecks else {
          continuation.resume(throwing: AuthoritativeContentVerificationError.capacityExceeded); return
        }
        let now = Date()
        cache = cache.filter { $0.value.expiresAt > now }
        let cached = cache[entryURI]?.proof
        let task = Task { [check] in
          do {
            let proof = try await check.run(scope, cachedProof: cached)
            try Task.checkCancellation()
            self.finish(entryURI, result: .success(proof))
          } catch { self.finish(entryURI, result: .failure(error)) }
        }
        flights[entryURI] = Flight(task: task, waiters: [id: continuation])
      }
    } onCancel: {
      Task { await self.cancelWaiter(entryURI, id: id) }
    }
  }

  public func shutdown() async {
    stopped = true
    let pending = flights.values.map(\.task)
    for task in pending { task.cancel() }
    for task in pending { await task.value }
    cache.removeAll()
  }

  private func cancelWaiter(_ uri: String, id: UUID) {
    guard var flight = flights[uri], let continuation = flight.waiters.removeValue(forKey: id) else { return }
    continuation.resume(throwing: CancellationError())
    // Retain admission until the cancelled request has joined DNS, HTTP and projection work.
    if flight.waiters.isEmpty { flight.task.cancel() }
    flights[uri] = flight
  }

  private func finish(_ uri: String, result: Result<AuthoritativeContentCheck.Proof, any Error>) {
    guard let flight = flights.removeValue(forKey: uri) else { return }
    if case .success(let proof) = result {
      if proof.present, !proof.fromCache {
        cache[uri] = Cached(proof: proof, expiresAt: Date().addingTimeInterval(cacheTTL))
        if cache.count > 512, let oldest = cache.min(by: { $0.value.expiresAt < $1.value.expiresAt })?.key {
          cache[oldest] = nil
        }
      } else if !proof.present { cache[uri] = nil }
    }
    for continuation in flight.waiters.values {
      continuation.resume(with: result.map(\.present))
    }
  }
}
