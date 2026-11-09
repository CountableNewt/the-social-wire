import Foundation
import SocialWireRedis

actor EdgeDraftAdmission {
  enum Failure: Error, Equatable { case limited, unavailable }
  struct Lease: Sendable { let id: UUID }
  private struct Bucket {
    var tokens: Double
    var updated: Date
    mutating func refill(now: Date, capacity: Double, rate: Double) {
      tokens = min(capacity, tokens + max(0, now.timeIntervalSince(updated)) * rate)
      updated = now
    }
  }
  private struct Owner {
    var requests: Bucket
    var transfers: Bucket
    var active = 0
  }
  private struct Claim {
    let owner: String
    let transfer: Bool
    var shared = false
  }
  private let redis: (any RedisCommandClient)?
  private let requiresRedis: Bool
  private let namespace: RedisKeyNamespace
  private var owners: [String: Owner] = [:]
  private var claims: [UUID: Claim] = [:]
  private var active = 0

  init(redis: (any RedisCommandClient)? = nil, requiresRedis: Bool = false, environment: String = "local") {
    self.redis = redis
    self.requiresRedis = requiresRedis
    self.namespace = RedisKeyNamespace(environment: environment)
  }

  func acquire(owner: String, transfer: Bool, now: Date = Date()) async throws -> Lease {
    if requiresRedis && redis == nil { throw Failure.unavailable }
    if owners.count >= 20_000 {
      owners = owners.filter { $0.value.active > 0 || now.timeIntervalSince($0.value.requests.updated) <= 120 }
      if owners[owner] == nil && owners.count >= 20_000 { throw Failure.limited }
    }
    var state = owners[owner] ?? Owner(requests: Bucket(tokens: 120, updated: now), transfers: Bucket(tokens: 32, updated: now))
    state.requests.refill(now: now, capacity: 120, rate: 10)
    state.transfers.refill(now: now, capacity: 32, rate: 1)
    guard !transfer || (active < 8 && state.active < 2) else { throw Failure.limited }
    if redis == nil {
      guard state.requests.tokens >= 1, !transfer || state.transfers.tokens >= 1 else { throw Failure.limited }
      state.requests.tokens -= 1
      if transfer { state.transfers.tokens -= 1 }
    }
    if transfer { active += 1; state.active += 1 }
    owners[owner] = state
    let lease = Lease(id: UUID())
    claims[lease.id] = Claim(owner: owner, transfer: transfer)
    guard let redis else { return lease }
    do {
      let keys = keys(owner: owner)
      let arguments: [RedisCommandValue] = [.data(Data(EdgeDraftAdmissionScript.acquire.utf8)), .integer(4)]
        + keys.map { .data(Data($0.utf8)) }
        + [.data(Data((transfer ? "1" : "0").utf8)), .data(Data(token(lease).utf8)), .integer(125_000)]
      let result = try await redis.execute(command: "EVAL", arguments: arguments)
      guard case .integer(let allowed) = result else { throw Failure.unavailable }
      guard allowed == 1 else { throw Failure.limited }
      claims[lease.id]?.shared = true
      try Task.checkCancellation()
      return lease
    } catch {
      await release(lease)
      if error is CancellationError { throw error }
      throw (error as? Failure) ?? Failure.unavailable
    }
  }

  func release(_ lease: Lease) async {
    guard let claim = claims.removeValue(forKey: lease.id) else { return }
    if claim.transfer {
      active -= 1
      owners[claim.owner]?.active -= 1
    }
    guard claim.shared, claim.transfer, let redis else { return }
    let keys = keys(owner: claim.owner)
    let token = token(lease)
    // The lease cleanup must survive cancellation of its request. The Redis
    // client's finite command timeout bounds this joined, unstructured cleanup.
    let cleanup = Task {
      for key in keys.suffix(2) {
        _ = try? await redis.execute(command: "ZREM", arguments: [.data(Data(key.utf8)), .data(Data(token.utf8))])
      }
    }
    await cleanup.value
  }

  private func token(_ lease: Lease) -> String { lease.id.uuidString.replacingOccurrences(of: "-", with: "").lowercased() }
  private func keys(owner: String) -> [String] {
    [
      namespace.key(domain: "social-draft-admission", safeComponents: ["requests"], identifiers: [owner]),
      namespace.key(domain: "social-draft-admission", safeComponents: ["uploads"], identifiers: [owner]),
      namespace.key(domain: "social-draft-admission", safeComponents: ["active-uploads"], identifiers: []),
      namespace.key(domain: "social-draft-admission", safeComponents: ["active-owner"], identifiers: [owner]),
    ]
  }
}
