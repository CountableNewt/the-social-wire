import Foundation

struct RepositorySnapshotRecord: Sendable {
  let uri: String
  let cid: String
  let value: Data
}
