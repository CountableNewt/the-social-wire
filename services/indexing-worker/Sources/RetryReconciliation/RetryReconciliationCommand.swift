import Foundation
import IndexingWorkerCore
import IndexingWorkerHost

#if canImport(Glibc)
  import Glibc
#else
  import Darwin
#endif

@main
struct RetryReconciliationCommand {
  static func main() async {
    do {
      let scope = try ReconciliationRetryScope(arguments: Array(CommandLine.arguments.dropFirst()))
      // Deliberately do not fall back to DATABASE_URL or put credentials in arguments/output.
      let databaseURL =
        ProcessInfo.processInfo.environment["RECONCILIATION_RETRY_DATABASE_URL"] ?? ""
      try await IndexingWorkerSignals.run(signalIsCancellation: true) {
        let count = try await ReconciliationRetryRunner.run(scope: scope, databaseURL: databaseURL)
        print("requeued_requests=\(count) historical_attempts_preserved=true")
      }
    } catch {
      let category: String
      switch error as? ReconciliationRetryError {
      case .invalidScope: category = "invalid_scope"
      case .expectedCountMismatch: category = "expected_count_mismatch"
      default: category = "retry_failed"
      }
      FileHandle.standardError.write(Data((category + "\n").utf8))
      exit(1)
    }
  }
}
