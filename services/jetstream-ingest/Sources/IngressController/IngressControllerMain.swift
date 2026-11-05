import Foundation
import Hummingbird
import IngressCore

#if canImport(Darwin)
  import Darwin
#else
  import Glibc
#endif

@main
struct IngressControllerMain {
  static func main() async {
    do {
      let config = try IngressConfiguration(environment: ProcessInfo.processInfo.environment)
      let database = try IngressDatabase(configuration: config)
      let health = IngressHealth()
      let router = Router()
      router.get("/healthz") { _, _ -> Response in
        Response(
          status: .ok, headers: [.contentType: "application/json"],
          body: .init(byteBuffer: .init(string: #"{"status":"ok"}"#)))
      }
      router.get("/startupz") { _, _ -> Response in
        let value = await health.snapshot()
        return Response(
          status: value.database ? .ok : .serviceUnavailable,
          headers: [.contentType: "application/json"],
          body: .init(
            byteBuffer: .init(
              bytes: try JSONEncoder().encode(
                await health.document(controller: !config.legacySingleLane)))))
      }
      router.get("/readyz") { _, _ -> Response in
        let value = await health.snapshot()
        return Response(
          status: value.ready ? .ok : .serviceUnavailable,
          headers: [.contentType: "application/json"],
          body: .init(
            byteBuffer: .init(
              bytes: try JSONEncoder().encode(
                await health.document(controller: !config.legacySingleLane)))))
      }
      router.get("/status") { _, _ -> Response in
        return Response(
          status: .ok, headers: [.contentType: "application/json"],
          body: .init(
            byteBuffer: .init(
              bytes: try JSONEncoder().encode(
                await health.document(controller: !config.legacySingleLane)))))
      }
      let app = Application(
        router: router, configuration: .init(address: .hostname("0.0.0.0", port: config.port)),
        services: [
          database.client,
          IngressSupervisor(configuration: config, database: database, health: health),
        ])
      try await app.runService()
    } catch {
      FileHandle.standardError.write(
        Data("Ingress controller stopped; configuration or runtime failure\n".utf8))
      exit(1)
    }
  }
}
