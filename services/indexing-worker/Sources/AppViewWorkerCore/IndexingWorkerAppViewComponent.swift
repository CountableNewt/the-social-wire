import AsyncHTTPClient
import IndexingWorkerCore
import Logging
import OperationsCore
import ThinAppViewCore

struct IndexingWorkerAppViewComponent: IndexingWorkerComponent {
  let environment: [String: String]
  let role: IndexingWorkerRole
  let healthPort: Int
  let client: HTTPClient
  let logger: Logger

  func run(authority: IndexingWorkerLeaseAuthority?) async throws {
    let native = authority.map {
      RoleLeaseAuthority(
        environment: $0.environment, role: $0.role, ownerID: $0.ownerID,
        fencingToken: $0.fencingToken)
    }
    try await AppViewWorkerHost.run(
      environment: environment,
      role: role == .projection ? .projection : .coordinator,
      serviceName: role == .projection ? "projection-pool-appview" : "coordinator-appview",
      healthListener: .enabled(hostname: "127.0.0.1", port: healthPort),
      roleLeaseAuthority: native, logger: logger)
  }

  func startup() async throws {
    try await IndexingWorkerLocalHealthProbe.run(
      client: client, port: healthPort, path: "/startupz")
  }

  func ready() async throws {
    try await IndexingWorkerLocalHealthProbe.run(client: client, port: healthPort, path: "/readyz")
  }
}
