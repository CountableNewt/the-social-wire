import Foundation

enum RepositorySnapshotError: Error, Sendable, Equatable {
  case invalid(String)
}
