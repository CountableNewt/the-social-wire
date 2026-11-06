import Foundation

#if canImport(Darwin)
  import Darwin
#else
  import Glibc
#endif

/// A disposable local server with an untrusted certificate whose CN does not
/// match the 127.0.0.1 client endpoint. No hosted database is used.
final class SchemaTLSDockerFixture {
  enum Failure: Error {
    case command(String)
    case startup, port
  }
  let name = "schema-ready-tls-" + UUID().uuidString.lowercased()
  let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
  private(set) var port = 0

  func start() throws {
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    _ = try command(
      "openssl",
      [
        "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
        "-subj", "/CN=localhost", "-keyout", directory.appendingPathComponent("server.key").path,
        "-out", directory.appendingPathComponent("server.crt").path,
      ], timeout: 15)
    try Data(
      "local all all trust\nhostssl all all 0.0.0.0/0 trust\nhostnossl all all 0.0.0.0/0 reject\n"
        .utf8
    )
    .write(to: directory.appendingPathComponent("pg_hba.conf"))
    let startup = """
      mkdir -p /fixture
      for attempt in {1..300}; do
        test -f /fixture/ready && break
        sleep 0.1
      done
      test -f /fixture/ready
      cp /fixture/server.key /fixture/server.crt /fixture/pg_hba.conf /tmp/
      chown postgres:postgres /tmp/server.key
      chmod 600 /tmp/server.key
      exec docker-entrypoint.sh postgres -c ssl=on -c ssl_cert_file=/tmp/server.crt -c ssl_key_file=/tmp/server.key -c hba_file=/tmp/pg_hba.conf
      """
    _ = try command(
      "docker",
      [
        "run", "--detach", "--name", name,
        "--publish", "127.0.0.1::5432", "--env", "POSTGRES_HOST_AUTH_METHOD=trust",
        "postgres:17", "bash", "-ec", startup,
      ], timeout: 60)
    _ = try command("docker", ["cp", directory.path + "/.", name + ":/fixture"], timeout: 10)
    _ = try command("docker", ["exec", name, "touch", "/fixture/ready"], timeout: 5)
    let deadline = Date().addingTimeInterval(45)
    var ready = false
    while Date() < deadline {
      if (try? command(
        "docker", ["exec", name, "pg_isready", "-h", "127.0.0.1", "-U", "postgres"], timeout: 3))
        != nil
      {
        ready = true
        break
      }
      Thread.sleep(forTimeInterval: 0.1)
    }
    guard ready else { throw Failure.startup }
    let mapping = try command("docker", ["port", name, "5432/tcp"], timeout: 5)
    guard let value = mapping.split(separator: ":").last,
      let port = Int(value.trimmingCharacters(in: .whitespacesAndNewlines))
    else { throw Failure.port }
    self.port = port
    _ = try command(
      "docker",
      [
        "exec", name, "psql", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-c",
        "CREATE TABLE public.schema_migrations(version text PRIMARY KEY); INSERT INTO public.schema_migrations VALUES ('20260815190000'), ('20261105000001');",
      ], timeout: 10)
  }

  func stop() {
    _ = try? command("docker", ["rm", "--force", name], timeout: 10)
    try? FileManager.default.removeItem(at: directory)
  }

  func url(mode: String?) -> String {
    // A containerized test runner reaches the same loopback-published port through its host.
    let host = ProcessInfo.processInfo.environment["SCHEMA_READY_TLS_TEST_HOST"] ?? "127.0.0.1"
    return "postgres://postgres@\(host):\(port)/postgres" + (mode.map { "?sslmode=" + $0 } ?? "")
  }

  private func command(_ executable: String, _ arguments: [String], timeout: TimeInterval) throws
    -> String
  {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = [executable] + arguments
    let output = Pipe()
    let errors = Pipe()
    process.standardOutput = output
    process.standardError = errors
    try process.run()
    let deadline = Date().addingTimeInterval(timeout)
    while process.isRunning, Date() < deadline { Thread.sleep(forTimeInterval: 0.02) }
    if process.isRunning {
      process.terminate()
      let grace = Date().addingTimeInterval(1)
      while process.isRunning, Date() < grace { Thread.sleep(forTimeInterval: 0.02) }
      if process.isRunning { _ = kill(process.processIdentifier, SIGKILL) }
      process.waitUntilExit()
      throw Failure.command(executable + " " + (arguments.first ?? "") + " timed out")
    }
    process.waitUntilExit()
    guard process.terminationStatus == 0 else {
      throw Failure.command(
        executable + " " + (arguments.first ?? "") + " exited " + String(process.terminationStatus))
    }
    return String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
  }
}
