/// Implementations must cancel and join the operation before releasing its lease.
/// A reacquired lease must supply a new authority rather than reusing a prior owner.
public protocol IndexingWorkerLeaseSupervisor: Sendable {
  func run(
    configuration: IndexingWorkerLeaseConfiguration,
    onEvent: @escaping @Sendable (IndexingWorkerLeaseEvent) async -> Void,
    operation: @escaping @Sendable (IndexingWorkerLeaseAuthority) async throws -> Void
  ) async throws
}
