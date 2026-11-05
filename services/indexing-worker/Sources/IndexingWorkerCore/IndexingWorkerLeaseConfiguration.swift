import Foundation

public struct IndexingWorkerLeaseConfiguration: Sendable {
  public let role: String
  public let ownerID: String
  public let leaseDuration: TimeInterval
  public let renewInterval: TimeInterval
  public let standbyRetryInterval: TimeInterval

  init(lane: IndexingWorkerLane, worker: IndexingWorkerConfig) {
    role = lane.coordinatorRole
    ownerID = worker.ownerID
    leaseDuration = worker.leaseDuration
    renewInterval = worker.leaseRenewInterval
    standbyRetryInterval = worker.standbyRetryInterval
  }
}
