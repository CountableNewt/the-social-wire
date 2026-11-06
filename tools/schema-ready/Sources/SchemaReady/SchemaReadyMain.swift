import Foundation
import SchemaReadyCore

#if canImport(Darwin)
  import Darwin
#else
  import Glibc
#endif

@main
struct SchemaReadyMain {
  static func main() async {
    do { try await run() } catch {
      FileHandle.standardError.write(Data(SchemaReadinessError.startupDiagnostic(for: error).utf8))
      exit(1)
    }
  }
  static func run() async throws {
    var arguments = Array(CommandLine.arguments.dropFirst())
    if arguments.count == 2 && arguments[0] == "--generate-manifest" {
      print(try SchemaManifest.generate(directory: arguments[1]).joined(separator: "\n"))
      return
    }
    let allowLocal = arguments.first == "--allow-local-sqlite"
    if allowLocal { arguments.removeFirst() }
    guard arguments.count >= 2, arguments[0] == "--", arguments[1].hasPrefix("/") else {
      throw SchemaReadinessError.invalidConfiguration(
        "schema-ready requires -- and an absolute application path")
    }
    if let url = try SchemaManifest.databaseURL(
      environment: ProcessInfo.processInfo.environment, allowLocalSQLite: allowLocal)
    {
      try await SchemaReadiness.wait(
        url: url, versions: SchemaManifest.read("/etc/socialwire/required-migrations.txt"))
    }
    try Task.checkCancellation()
    let values = arguments.dropFirst().map { strdup($0) }
    defer { for value in values { free(value) } }
    var argv = values + [nil]
    _ = argv.withUnsafeMutableBufferPointer { pointer in execv(arguments[1], pointer.baseAddress!) }
    throw SchemaReadinessError.invalidConfiguration(
      "Cannot execute application after schema readiness")
  }
}
