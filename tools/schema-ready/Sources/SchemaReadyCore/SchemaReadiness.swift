import Foundation
import PostgresNIO

public enum SchemaReadiness {
  public static func wait(url: String, versions: [String], timeout: Duration = .seconds(90))
    async throws
  {
    let required = try SchemaManifest.validate(versions)
    let configuration = try SchemaPostgresConfiguration.make(url: url)
    let client = PostgresClient(configuration: configuration)
    try await withThrowingTaskGroup(of: Void.self) { group in
      group.addTask { await client.run() }
      group.addTask {
        let deadline = ContinuousClock.now.advanced(by: timeout)
        while ContinuousClock.now < deadline {
          try Task.checkCancellation()
          do {
            let rows = try await client.query(
              "SELECT COUNT(*) FROM unnest(\(required)::text[]) AS required(version) WHERE NOT EXISTS(SELECT 1 FROM public.schema_migrations applied WHERE applied.version=required.version)"
            )
            for try await row in rows {
              if try row.decode(Int64.self) == 0 { return }
            }
          } catch let error as PSQLError {
            if ["42501", "28P01", "28000"].contains(error.serverInfo?[.sqlState] ?? "") {
              throw SchemaReadinessError.invalidConfiguration(
                "Schema readiness database permission or authentication denied")
            }
          }
          try await Task.sleep(for: .seconds(1))
        }
        throw SchemaReadinessError.timeout
      }
      group.addTask {
        try await Task.sleep(for: timeout)
        throw SchemaReadinessError.timeout
      }
      defer { group.cancelAll() }
      _ = try await group.next()
    }
  }
}
