import Foundation
import Testing

@testable import SchemaReadyCore

struct SchemaManifestTests {
  @Test func requiresEveryUniqueCanonicalReceipt() throws {
    #expect(
      try SchemaManifest.validate(["20261105000001", "20261104000001"]) == [
        "20261104000001", "20261105000001",
      ])
    for versions in [
      [], ["20261105000001", "20261105000001"], ["2026110500000"], ["2026110500000x"],
    ] {
      #expect(throws: (any Error).self) { try SchemaManifest.validate(versions) }
    }
  }

  @Test func manifestIncludesOlderMigrationsAndIgnoresOtherFiles() throws {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: directory) }
    for name in ["20261105000001_latest.sql", "20260815190000_older.sql", "README.md"] {
      try Data().write(to: directory.appendingPathComponent(name))
    }
    #expect(
      try SchemaManifest.generate(directory: directory.path) == [
        "20260815190000", "20261105000001",
      ])
    try Data().write(to: directory.appendingPathComponent("20260815190000_duplicate.sql"))
    #expect(throws: (any Error).self) { try SchemaManifest.generate(directory: directory.path) }
  }

  @Test func hostedStartupCannotBypassTheDatabaseGate() throws {
    for environment in [
      ["APP_ENV": "local", "RAILWAY_SERVICE_ID": "service"],
      ["APP_ENV": "local", "RAILWAY_ENVIRONMENT_ID": "environment"],
      ["APP_ENV": "dev"], ["APP_ENV": "production", "DATABASE_URL": "postgres://fixture/db"],
    ] {
      #expect(throws: (any Error).self) {
        try SchemaManifest.databaseURL(environment: environment, allowLocalSQLite: true)
      }
    }
    #expect(
      try SchemaManifest.databaseURL(environment: ["APP_ENV": "local"], allowLocalSQLite: true)
        == nil)
    #expect(throws: (any Error).self) {
      try SchemaManifest.databaseURL(environment: ["APP_ENV": " local "], allowLocalSQLite: true)
    }
    #expect(throws: (any Error).self) {
      try SchemaManifest.databaseURL(environment: ["APP_ENV": "local"], allowLocalSQLite: false)
    }
    #expect(
      try SchemaManifest.databaseURL(
        environment: ["APP_ENV": "prod", "DATABASE_URL": " postgres://fixture/db "],
        allowLocalSQLite: false
      ) == "postgres://fixture/db")
  }

  @Test func databaseSessionIsReadOnlyAndBounded() throws {
    let configuration = try SchemaPostgresConfiguration.make(
      url: "postgres://viewer:password@localhost/database?sslmode=disable")
    let parameters = Dictionary(
      uniqueKeysWithValues: configuration.options.additionalStartupParameters)
    #expect(parameters["default_transaction_read_only"] == "on")
    #expect(parameters["statement_timeout"] == "5000")
    #expect(parameters["lock_timeout"] == "1000")
    #expect(configuration.options.maximumConnections == 1)
    #expect(configuration.options.minimumConnections == 0)
    for url in [
      "https://localhost/database", "postgres:///database", "postgres://viewer@localhost/",
      "postgres://viewer@localhost/database?sslmode=unknown",
    ] {
      #expect(throws: (any Error).self) { try SchemaPostgresConfiguration.make(url: url) }
    }
  }
}
