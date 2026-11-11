import Foundation
import GatewayCore
import Hummingbird

struct PublicationRoutes {
  let projectionService: PublicationProjectionService
  let resolveService: PublicationResolveService

  func register(on group: RouterGroup<GatewayRequestContext>) {
    for path in [
      "/v1/publications/sidebar",
      "/xrpc/app.thesocialwire.publication.getSidebar",
    ] as [RouterPath] {
      group.get(path) { request, context async throws -> PublicationSidebarResponse in
        guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
        let phase: SidebarBuildPhase
        switch request.uri.queryParameters.get("phase") {
        case "priority": phase = .priority
        case "folderPublications": phase = .folderPublications
        default: phase = .full
        }
        return try await projectionService.sidebar(auth: auth, phase: phase)
      }
    }

    // Refresh the subscribed folder contents now and update Following in the background.
    for path in [
      "/v1/publications/refresh",
      "/xrpc/app.thesocialwire.publication.refreshSidebar",
    ] as [RouterPath] {
      group.post(path) { _, context async throws -> PublicationSidebarResponse in
        guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
        let response = try await projectionService.refreshSidebar(auth: auth)
        Task { await projectionService.refreshFullDiscoveryAndPersist(auth: auth) }
        return response
      }
    }

    for path in [
      "/v1/publications/resolve",
      "/xrpc/app.thesocialwire.publication.resolvePublication",
    ] as [RouterPath] {
      group.post(path) { request, context async throws -> ResolveAddPublicationResponse in
        guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
        let body = try await request.decode(as: ResolveAddPublicationRequest.self, context: context)
        return await resolveService.resolve(input: body.input, auth: auth)
      }
    }
  }
}
