import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird

struct PodcastMediaProxyRoutes {
  let baseURL: String
  let internalSecret: String?
  let httpClient: HTTPClient
  func register(
    on protected: RouterGroup<GatewayRequestContext>, router: Router<GatewayRequestContext>
  ) {
    protected.get("/xrpc/app.thesocialwire.podcast.getImage") { request, context async throws -> Response in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      return try await forward(request: request, path: "/xrpc/app.thesocialwire.podcast.getImage", auth: auth)
    }
    protected.get("/xrpc/app.thesocialwire.podcast.getMedia") { request, context async throws -> Response in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      return try await forward(request: request, path: "/xrpc/app.thesocialwire.podcast.getMedia", auth: auth)
    }
    protected.get("/xrpc/app.thesocialwire.podcast.getClipAsset") { request, context async throws -> Response in
      guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
      return try await forward(request: request, path: "/xrpc/app.thesocialwire.podcast.getClipAsset", auth: auth)
    }
    router.get("/xrpc/app.thesocialwire.podcast.getPublicClipAsset") { request, _ async throws -> Response in
      try await forward(request: request, path: "/xrpc/app.thesocialwire.podcast.getPublicClipAsset", auth: nil)
    }
    router.get("/xrpc/app.thesocialwire.podcast.getPublicClip") { request, _ async throws -> Response in
      try await forward(request: request, path: "/xrpc/app.thesocialwire.podcast.getPublicClip", auth: nil)
    }
  }
  private func forward(request: Request, path: String, auth: AuthContext?) async throws -> Response
  {
    let query = GatewayInternalTrust.canonicalPathWithQuery(path: path, query: request.uri.query)
    var req = HTTPClientRequest(
      url: baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + query)
    if let range = request.headers[.range] { req.headers.add(name: "Range", value: range) }
    if let auth {
      req.headers.add(name: "Authorization", value: auth.authorizationForwardingValue)
      if let dpop = auth.dpopProof { req.headers.add(name: "DPoP", value: dpop) }
      if let upstream = auth.upstreamDpopProof?.trimmingCharacters(in: .whitespacesAndNewlines),
        !upstream.isEmpty
      {
        req.headers.add(name: ATProtoUpstreamDPoP.headerName, value: upstream)
      }
    }
    if let auth, let secret = internalSecret {
      let headers = try GatewayInternalTrust.signedHeaders(
        secret: secret, did: auth.did, method: "GET", pathWithQuery: path)
      for h in headers { req.headers.add(name: h.name, value: h.value) }
    }
    let reply = try await httpClient.execute(req, timeout: .hours(6))
    var headers = HTTPFields()
    for name in ["Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "X-Content-Type-Options"] {
      if let field = HTTPField.Name(name), let value = reply.headers.first(name: name) {
        headers[field] = value
      }
    }
    headers[.cacheControl] = "private, no-store"
    return Response(
      status: HTTPResponse.Status(code: Int(reply.status.code)), headers: headers,
      body: ResponseBody { writer in
        for try await chunk in reply.body { try await writer.write(chunk) }
        try await writer.finish(nil)
      })
  }
}
