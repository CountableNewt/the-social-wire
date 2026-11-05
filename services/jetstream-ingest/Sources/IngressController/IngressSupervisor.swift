import Foundation
import IngressCore
import ServiceLifecycle

struct IngressSupervisor: Service {
  let configuration: IngressConfiguration
  let database: IngressDatabase
  let health: IngressHealth
  func run() async throws {
    while !Task.isCancelled {
      do {
        try await IngressRunner(configuration: configuration, database: database, health: health)
          .run()
      } catch {
        if Task.isCancelled { return }
        await health.failed("AppView ingress failed; waiting before supervised restart")
        if case IngressError.sourceDiscontinuity = error {
          await health.paused(true)
          while !Task.isCancelled { try await Task.sleep(for: .seconds(60)) }
        }
        try await Task.sleep(for: .seconds(1))
      }
    }
  }
}
