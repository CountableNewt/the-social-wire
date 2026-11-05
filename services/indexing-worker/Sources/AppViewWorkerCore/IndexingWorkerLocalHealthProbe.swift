import AsyncHTTPClient
import IndexingWorkerCore
import NIOHTTP1

enum IndexingWorkerLocalHealthProbe {
  static func run(
    client: HTTPClient,
    port: Int,
    path: String
  ) async throws {
    var request = HTTPClientRequest(url: "http://127.0.0.1:\(port)\(path)")
    request.method = .GET
    let response = try await client.execute(request, timeout: .seconds(2))
    _ = try await response.body.collect(upTo: 16 * 1_024)
    guard response.status == .ok else {
      throw IndexingWorkerHealthError.componentUnavailable(port: port, path: path)
    }
  }
}

enum IndexingWorkerHealthError: Error, Equatable {
  case componentUnavailable(port: Int, path: String)
  case laneNotStarted(IndexingWorkerLane)
  case laneRestarting(IndexingWorkerLane)
}
