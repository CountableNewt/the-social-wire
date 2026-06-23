import Foundation

/// SQLite or Postgres database backend for Thin AppView persistence.
public enum DatabaseBackend: Sendable {
  case sqlite(path: String)
  case postgres(url: String)

  public static func fromEnvironment(
    _ env: [String: String] = ProcessInfo.processInfo.environment
  ) -> DatabaseBackend {
    let appEnv = env["APP_ENV"] ?? "local"
    switch appEnv {
    case "local":
      return .sqlite(path: env["SQLITE_DB_PATH"] ?? "./social-wire.sqlite")

    default:
      guard let dbURL = env["SUPABASE_DATABASE_URL"], !dbURL.isEmpty else {
        fatalError("SUPABASE_DATABASE_URL is required for APP_ENV=\(appEnv)")
      }
      return .postgres(url: dbURL)
    }
  }
}
