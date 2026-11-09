import AsyncHTTPClient
import Foundation
import Hummingbird

/// A responsive API cannot advertise readiness while its in-container media
/// processor is starting, unhealthy, or unable to answer within a short deadline.
struct StandaloneMediaReadinessProbe: Sendable {
  typealias Check = @Sendable (String) async throws -> (status: Int, body: Data)
  let baseURL: String?
  let check: Check

  init(baseURL: String?, httpClient: HTTPClient) {
    self.baseURL = baseURL
    self.check = { url in
      let response = try await httpClient.execute(HTTPClientRequest(url: url), timeout: .seconds(2))
      let bytes = try await response.body.collect(upTo: 1024)
      return (Int(response.status.code), Data(bytes.readableBytesView))
    }
  }

  init(baseURL: String?, check: @escaping Check) {
    self.baseURL = baseURL
    self.check = check
  }

  func run() async throws {
    guard let raw = baseURL, let components = URLComponents(string: raw),
      components.scheme == "http", let host = components.host,
      ["127.0.0.1", "localhost", "::1"].contains(host),
      components.user == nil, components.password == nil, components.query == nil,
      components.fragment == nil, components.path.isEmpty || components.path == "/"
    else { throw unavailable() }
    let origin = raw.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    do {
      async let startup = check(origin + "/startupz")
      async let health = check(origin + "/healthz")
      let (startupResult, healthResult) = try await (startup, health)
      guard startupResult.status == 200, healthResult.status == 200,
        let startupBody = try JSONSerialization.jsonObject(with: startupResult.body)
          as? [String: Any],
        startupBody["ready"] as? Bool == true,
        let healthBody = try JSONSerialization.jsonObject(with: healthResult.body)
          as? [String: Any],
        healthBody["status"] as? String == "ok"
      else { throw unavailable() }
    } catch is CancellationError { throw CancellationError() } catch { throw unavailable() }
  }

  private func unavailable() -> HTTPError {
    HTTPError(.serviceUnavailable, message: "Podcast media processor unavailable")
  }
}
