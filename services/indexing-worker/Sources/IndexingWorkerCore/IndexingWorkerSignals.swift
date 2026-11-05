import Foundation

#if canImport(Glibc)
  import Glibc
#else
  import Darwin
#endif

/// Executable-only process signal ownership. Library consumers cancel tasks
/// directly; they must not change the embedding process's signal handlers.
public enum IndexingWorkerSignals {
  public static func run(
    signalIsCancellation: Bool = false, operation: @escaping @Sendable () async throws -> Void
  ) async throws {
    let (events, continuation) = AsyncStream<Void>.makeStream(bufferingPolicy: .bufferingNewest(1))
    let priorTerm = signal(SIGTERM, SIG_IGN)
    let priorInterrupt = signal(SIGINT, SIG_IGN)
    let sources = [SIGTERM, SIGINT].map { number in
      let source = DispatchSource.makeSignalSource(signal: number, queue: .global())
      source.setEventHandler { continuation.yield(()) }
      source.resume()
      return source
    }
    defer {
      for source in sources { source.cancel() }
      continuation.finish()
      signal(SIGTERM, priorTerm)
      signal(SIGINT, priorInterrupt)
    }
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { try await operation() }
      group.addTask {
        for await _ in events {
          if signalIsCancellation { throw CancellationError() }
          return
        }
      }
      defer { group.cancelAll() }
      try await group.next()
    }
  }
}
