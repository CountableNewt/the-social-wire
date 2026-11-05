import Foundation
import IndexingWorkerCore
import Logging
import PostgresNIO
import ThinAppViewCore

/// An explicit operator action. No worker startup path invokes this command.
public enum ReconciliationRetryRunner {
  static let logger = Logger(
    label: "reconciliation-retry", factory: { _ in SwiftLogNoOpLogHandler() })

  public static func run(scope: ReconciliationRetryScope, databaseURL: String) async throws -> Int {
    guard !databaseURL.isEmpty, let url = URL(string: databaseURL),
      ["postgres", "postgresql"].contains(url.scheme), url.host?.isEmpty == false
    else { throw ReconciliationRetryError.invalidScope }
    var configuration: PostgresClient.Configuration
    do { configuration = try makePostgresConfig(from: databaseURL, logger: logger) } catch {
      throw ReconciliationRetryError.invalidScope
    }
    configuration.options.maximumConnections = 1
    configuration.options.connectTimeout = .seconds(5)
    configuration.options.additionalStartupParameters = [
      ("application_name", "socialwire-operator-reconciliation-retry")
    ]
    let pool = PostgresClient(configuration: configuration, backgroundLogger: logger)
    return try await withThrowingTaskGroup(of: Int.self) { group in
      group.addTask {
        await pool.run()
        throw CancellationError()
      }
      group.addTask { try await apply(scope: scope, pool: pool, at: Date()) }
      group.addTask {
        try await Task.sleep(for: .seconds(30))
        throw CancellationError()
      }
      defer { group.cancelAll() }
      guard let result = try await group.next() else { throw CancellationError() }
      return result
    }
  }

  static func apply(scope: ReconciliationRetryScope, pool: PostgresClient, at: Date) async throws
    -> Int
  {
    do {
      return try await pool.withTransaction(logger: logger) { connection in
        try await connection.query("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE", logger: logger)
        try await connection.query("SET LOCAL lock_timeout = '5s'", logger: logger)
        try await connection.query("SET LOCAL statement_timeout = '10s'", logger: logger)
        let rows = try await connection.query(
          """
          SELECT request.id FROM appview_ingestion_reconciliation_requests request
          JOIN appview_ingestion_inbox inbox
            ON inbox.environment = request.environment
           AND inbox.source_generation = request.source_generation
           AND inbox.seq = request.trigger_seq AND inbox.repo_did = request.repo_did
          WHERE request.environment = \(scope.environment) AND request.source_generation = \(scope.generation)
            AND request.status = 'failed' AND inbox.status = 'dead_letter' AND inbox.reconciled_at IS NULL
          ORDER BY request.trigger_seq, request.id
          LIMIT \(scope.expectedCount + 1) FOR UPDATE OF request, inbox
          """, logger: logger)
        var identifiers: [String] = []
        for try await row in rows { identifiers.append(try row.decode(String.self)) }
        guard identifiers.count == scope.expectedCount else {
          throw ReconciliationRetryError.expectedCountMismatch
        }
        let changed = try await connection.query(
          """
          UPDATE appview_ingestion_reconciliation_requests
          SET status = 'pending', next_attempt_at = \(at), lease_owner = NULL,
              lease_token = NULL, lease_expires_at = NULL, updated_at = \(at)
          WHERE environment = \(scope.environment) AND source_generation = \(scope.generation)
            AND status = 'failed' AND id = ANY(\(identifiers)::text[])
          RETURNING id
          """, logger: logger)
        var count = 0
        for try await _ in changed { count += 1 }
        guard count == scope.expectedCount else {
          throw ReconciliationRetryError.expectedCountMismatch
        }
        return count
      }
    } catch let error as PostgresTransactionError {
      if let scoped = error.closureError as? ReconciliationRetryError { throw scoped }
      throw error
    }
  }
}
