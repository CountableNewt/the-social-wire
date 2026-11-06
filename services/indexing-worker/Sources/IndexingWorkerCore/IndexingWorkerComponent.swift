public protocol IndexingWorkerComponent: Sendable {
  func run(authority: IndexingWorkerLeaseAuthority?) async throws
  func startup() async throws
  func ready() async throws
}
