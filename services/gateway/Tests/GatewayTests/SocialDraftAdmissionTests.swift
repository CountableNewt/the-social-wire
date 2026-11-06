import Foundation
import Testing
@testable import Gateway

@Test func privateDraftTransfersRespectOwnerAndProcessLimitsAndIdempotentRelease() async throws {
  let admission = SocialDraftAdmission()
  let now = Date()
  let first = try await admission.acquire(owner: "did:plc:owner", transfer: true, now: now)
  let second = try await admission.acquire(owner: "did:plc:owner", transfer: true, now: now)
  await #expect(throws: SocialDraftAdmission.Failure.limited) {
    try await admission.acquire(owner: "did:plc:owner", transfer: true, now: now)
  }
  await admission.release(first)
  await admission.release(first)
  let third = try await admission.acquire(owner: "did:plc:owner", transfer: true, now: now)
  for number in 0..<6 { _ = try await admission.acquire(owner: "did:plc:other-\(number)", transfer: true, now: now) }
  await #expect(throws: SocialDraftAdmission.Failure.limited) {
    try await admission.acquire(owner: "did:plc:another", transfer: true, now: now)
  }
  await admission.release(second)
  await admission.release(third)
  _ = try await admission.acquire(owner: "did:plc:another", transfer: true, now: now)
}

@Test func privateDraftRequestBudgetRefillsAndConfiguredRedisNeverFallsBackLocally() async throws {
  let now = Date()
  let admission = SocialDraftAdmission()
  for _ in 0..<120 {
    let lease = try await admission.acquire(owner: "did:plc:owner", transfer: false, now: now)
    await admission.release(lease)
  }
  await #expect(throws: SocialDraftAdmission.Failure.limited) {
    try await admission.acquire(owner: "did:plc:owner", transfer: false, now: now)
  }
  _ = try await admission.acquire(owner: "did:plc:owner", transfer: false, now: now.addingTimeInterval(0.1))
  let unavailable = SocialDraftAdmission(requiresRedis: true)
  await #expect(throws: SocialDraftAdmission.Failure.unavailable) {
    try await unavailable.acquire(owner: "did:plc:owner", transfer: true, now: now)
  }
}
