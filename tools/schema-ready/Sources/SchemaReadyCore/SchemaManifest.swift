import Foundation

public enum SchemaManifest {
  public static func validate(_ versions: [String]) throws -> [String] {
    guard !versions.isEmpty else {
      throw SchemaReadinessError.invalidConfiguration("Migration manifest is empty")
    }
    let sorted = versions.sorted()
    guard Set(sorted).count == sorted.count,
      sorted.allSatisfy({ $0.utf8.count == 14 && $0.utf8.allSatisfy { $0 >= 48 && $0 <= 57 } })
    else {
      throw SchemaReadinessError.invalidConfiguration(
        "Migration manifest contains invalid or duplicate versions")
    }
    return sorted
  }
  public static func read(_ path: String) throws -> [String] {
    try validate(
      String(contentsOfFile: path, encoding: .utf8).split(whereSeparator: \.isWhitespace).map(
        String.init))
  }
  public static func generate(directory: String) throws -> [String] {
    try validate(
      FileManager.default.contentsOfDirectory(atPath: directory).filter { $0.hasSuffix(".sql") }.map
      {
        guard $0.contains("_") else {
          throw SchemaReadinessError.invalidConfiguration("Invalid migration filename")
        }
        return String($0.prefix(while: { $0 != "_" }))
      })
  }
  public static func databaseURL(environment env: [String: String], allowLocalSQLite: Bool) throws
    -> String?
  {
    let raw = env["APP_ENV"] ?? ""
    let appEnv = raw.trimmingCharacters(in: .whitespacesAndNewlines)
    if allowLocalSQLite && raw != appEnv {
      throw SchemaReadinessError.invalidConfiguration("Schema readiness requires canonical APP_ENV")
    }
    let hosted =
      !(env["RAILWAY_ENVIRONMENT_ID"] ?? "").isEmpty || !(env["RAILWAY_SERVICE_ID"] ?? "").isEmpty
    if allowLocalSQLite && !hosted && ["", "local"].contains(appEnv) { return nil }
    guard ["dev", "prod"].contains(appEnv), let url = env["DATABASE_URL"],
      !url.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    else {
      throw SchemaReadinessError.invalidConfiguration(
        "Schema readiness requires APP_ENV=dev|prod and DATABASE_URL")
    }
    return url.trimmingCharacters(in: .whitespacesAndNewlines)
  }
}
