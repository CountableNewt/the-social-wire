import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import NIOCore

struct GatewayOAuthMetadataRoutes {
  let publicOrigin: String?
  let nativeOrigin: String?
  let operationsOrigin: String?
  let podcastsEnabled: Bool

  func register(on router: Router<GatewayRequestContext>) {
    for path in ["/oauth-client-metadata.json", "/oauth/client-metadata.json", "/ios-client-metadata.json", "/operations-oauth-client-metadata.json"] {
      router.get(RouterPath(path)) { request, _ -> Response in
        let forwarded = HTTPField.Name("X-Forwarded-Proto")!
        let scheme = request.headers[forwarded]?.split(separator: ",").first.map(String.init)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "https"
        guard let host = request.head.authority, !host.isEmpty else {
          throw HTTPError(.badRequest, message: "Invalid metadata origin")
        }
        let document = try document(path: path, requestOrigin: "\(scheme)://\(host)")
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return Response(status: .ok, headers: [.contentType: "application/json", .accessControlAllowOrigin: "*"], body: .init(byteBuffer: ByteBuffer(bytes: try encoder.encode(document))))
      }
    }
  }

  func document(path: String, requestOrigin: String) throws -> GatewayOAuthMetadataDocument {
    let native = path == "/ios-client-metadata.json"
    let operations = path == "/operations-oauth-client-metadata.json"
    let clientOrigin = normalized(native ? (nativeOrigin ?? requestOrigin) : requestOrigin)
    let clientPath = native || operations ? path : "/oauth-client-metadata.json"
    let redirect: String
    if native {
      guard let host = URL(string: clientOrigin)?.host, !host.isEmpty else {
        throw HTTPError(.badRequest, message: "Invalid metadata origin")
      }
      redirect = host.split(separator: ".").reversed().joined(separator: ".") + ":/oauth/callback"
    } else if operations {
      guard let operationsOrigin, !operationsOrigin.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
        throw HTTPError(.serviceUnavailable, message: "Operations OAuth origin is not configured")
      }
      redirect = normalized(operationsOrigin) + "/callback"
    } else {
      redirect = normalized(publicOrigin ?? requestOrigin) + "/callback"
    }
    return GatewayOAuthMetadataDocument(
      client_id: clientOrigin + clientPath,
      application_type: native ? "native" : "web",
      redirect_uris: [redirect],
      scope: operations ? "atproto" : GatewayOAuthScopes.scope(native: native, podcastsEnabled: podcastsEnabled),
      client_name: operations ? "The Social Wire Operations" : "The Social Wire",
      client_uri: clientOrigin
    )
  }

  private func normalized(_ origin: String) -> String {
    var result = origin.trimmingCharacters(in: .whitespacesAndNewlines)
    while result.hasSuffix("/") { result.removeLast() }
    return result
  }
}
