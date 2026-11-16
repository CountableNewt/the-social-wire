import GatewayCore
import Hummingbird

struct PodcastPrivateRoutes {
  let service: PodcastService
  func register(on group: RouterGroup<GatewayRequestContext>) {
    group.post("/xrpc/app.thesocialwire.podcast.resolvePrivateShow") { request, context async throws -> PodcastResolveResponse in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      let body = try await request.decode(as: PodcastResolveRequest.self, context: context)
      return try await service.resolvePrivate(viewer: auth.did, raw: body.url)
    }
    group.post("/xrpc/app.thesocialwire.podcast.refreshPrivateShow") { request, context async throws -> PodcastResolveResponse in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      let body = try await request.decode(as: PodcastPrivateRefreshRequest.self, context: context)
      return try await service.refreshPrivate(viewer: auth.did, showID: body.showId)
    }
    group.post("/xrpc/app.thesocialwire.podcast.deletePrivateSubscription") { request, context async throws -> Response in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      let id = try await request.decode(as: PodcastPrivateRefreshRequest.self, context: context).showId
      guard try await service.store.removePrivateSubscription(viewer: auth.did, showID: id)
      else { throw HTTPError(.notFound) }
      return PodcastJSON.response("{}")
    }
  }
}
