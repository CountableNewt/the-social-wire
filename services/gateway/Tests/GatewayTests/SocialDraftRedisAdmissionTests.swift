import Foundation
import Logging
import SocialWireRedis
import Testing
@testable import Gateway

@Test(.enabled(if: ProcessInfo.processInfo.environment["TEST_SOCIAL_DRAFT_REDIS_URL"] != nil))
func privateDraftTransferSlotsAreSharedAcrossGatewayReplicas() async throws {
  let url = try #require(ProcessInfo.processInfo.environment["TEST_SOCIAL_DRAFT_REDIS_URL"])
  let redis = try RediStackRedisClient(configuration: RedisConfiguration(url: url, minimumConnectionCount: 1, maximumConnectionCount: 2, commandTimeoutMilliseconds: 250), logger: Logger(label: "draft-admission-integration"))
  let environment = "test-\(UUID().uuidString)"
  let first = SocialDraftAdmission(redis: redis, requiresRedis: true, environment: environment)
  let second = SocialDraftAdmission(redis: redis, requiresRedis: true, environment: environment)
  do {
    let one = try await first.acquire(owner: "did:plc:viewer", transfer: true)
    let two = try await first.acquire(owner: "did:plc:viewer", transfer: true)
    await #expect(throws: SocialDraftAdmission.Failure.limited) {
      try await second.acquire(owner: "did:plc:viewer", transfer: true)
    }
    await first.release(one)
    let replacement = try await second.acquire(owner: "did:plc:viewer", transfer: true)
    await first.release(two)
    await second.release(replacement)
  } catch { try? await redis.shutdown(); throw error }
  try await redis.shutdown()
}

@Test func configuredRedisCommandFailureFailsClosedAndReleasesLocalClaims() async {
  let admission = SocialDraftAdmission(redis: UnavailableDraftRedis(), requiresRedis: true)
  for _ in 0..<3 {
    await #expect(throws: SocialDraftAdmission.Failure.unavailable) {
      try await admission.acquire(owner: "did:plc:viewer", transfer: true)
    }
  }
}
