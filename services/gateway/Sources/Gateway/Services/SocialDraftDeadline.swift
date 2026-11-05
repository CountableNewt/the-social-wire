enum SocialDraftDeadline {
  static func run<Value: Sendable>(until deadline: ContinuousClock.Instant, operation: @escaping @Sendable () async throws -> Value) async throws -> Value {
    try await withThrowingTaskGroup(of: Value.self) { group in
      defer { group.cancelAll() }
      group.addTask { try await operation() }
      group.addTask {
        try await ContinuousClock().sleep(until: deadline)
        throw Failure.expired
      }
      guard let result = try await group.next() else { throw CancellationError() }
      try Task.checkCancellation()
      guard ContinuousClock.now < deadline else { throw Failure.expired }
      return result
    }
  }
  enum Failure: Error { case expired }
}
