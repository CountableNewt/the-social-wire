import Foundation

struct RepositorySnapshot: Sendable {
  let pdsBase: String
  let commitCID: String
  let revision: String
  let records: [RepositorySnapshotRecord]
}
