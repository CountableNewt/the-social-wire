import Foundation
import NIOConcurrencyHelpers

/// Synchronous release is intentional: a lease must also release on response-body deinit.
struct SocialDraftAdmission: Sendable {
  private let owners = NIOLockedValueBox([UUID: String]())
  let limit: Int

  func acquire(owner: String) throws -> SocialDraftAdmissionLease {
    try Task.checkCancellation()
    let id = UUID()
    try owners.withLockedValue { active in
      guard active.count < limit, owner.isEmpty || !active.values.contains(owner) else {
        throw SocialDraftStorageError.unavailable
      }
      active[id] = owner
    }
    return SocialDraftAdmissionLease { _ = owners.withLockedValue { $0.removeValue(forKey: id) } }
  }
}
