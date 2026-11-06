import Foundation

/// The exact owner and fencing generation granted by the authority store.
public struct IndexingWorkerLeaseAuthority: Sendable, Equatable {
  public let role: String
  public let environment: String
  public let ownerID: String
  public let fencingToken: Int64
  public init(environment: String, role: String, ownerID: String, fencingToken: Int64) {
    self.environment = environment
    self.role = role
    self.ownerID = ownerID
    self.fencingToken = fencingToken
  }
}
