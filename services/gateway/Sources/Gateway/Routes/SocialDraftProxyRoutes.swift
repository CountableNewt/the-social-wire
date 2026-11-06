import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import NIOCore

struct SocialDraftProxyRoutes {
  let baseURL: String
  let internalSecret: String?
  let httpClient: HTTPClient
  let admission: SocialDraftAdmission

  func register(on group: RouterGroup<GatewayRequestContext>) {
    registerArticleDrafts(on: group)
    for name in ["getAttachment", "getManifest"] {
      let path = "/xrpc/app.thesocialwire.socialDraft.\(name)"
      group.get(RouterPath(path)) { request, context in
        try await handle(request: request, context: context, path: path, method: "GET")
      }
    }
    for name in ["reserveAttachment", "uploadAttachment", "putManifest", "deleteManifest"] {
      let path = "/xrpc/app.thesocialwire.socialDraft.\(name)"
      group.post(RouterPath(path)) { request, context in
        try await handle(request: request, context: context, path: path, method: "POST")
      }
    }
  }

  private func registerArticleDrafts(on group: RouterGroup<GatewayRequestContext>) {
    for path in ["/xrpc/app.thesocialwire.articleDraft.list", "/xrpc/app.thesocialwire.articleDraft.get"] {
      group.get(RouterPath(path)) { request, context in
        try await handle(request: request, context: context, path: path, method: "GET")
      }
    }
    for path in ["/xrpc/app.thesocialwire.articleDraft.put", "/xrpc/app.thesocialwire.articleDraft.delete"] {
      group.post(RouterPath(path)) { request, context in
        try await handle(request: request, context: context, path: path, method: "POST")
      }
    }
  }

  private func handle(request: Request, context: GatewayRequestContext, path: String, method: String) async throws -> Response {
    guard let auth = context.authContext, !auth.did.isEmpty,
      auth.did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
    else { return failure(.unauthorized, "Authentication required") }
    let upload = path.hasSuffix(".uploadAttachment")
    let download = path.hasSuffix(".getAttachment")
    let deadline = ContinuousClock.now.advanced(by: .seconds(upload || download ? 120 : 60))
    let lease: SocialDraftAdmission.Lease
    do {
      lease = try await admission.acquire(owner: auth.did, transfer: upload || download)
    } catch let error as SocialDraftAdmission.Failure {
      var response = failure(error == .limited ? .tooManyRequests : .serviceUnavailable, error == .limited ? "Too many private draft requests" : "Private draft admission unavailable")
      response.headers[.retryAfter] = "1"
      return response
    }
    let lifetime = PrivateDraftLease(admission: admission, lease: lease)
    let expiry = Task {
      do {
        try await ContinuousClock().sleep(until: deadline)
        context.closeTransport()
      } catch { /* A completed exchange cancels the monitor. */ }
    }
    do {
      let response = try await withTaskCancellationHandler {
        try await SocialDraftDeadline.run(until: deadline) {
          try await forward(request: request, context: context, path: path, method: method, upload: upload, download: download, deadline: deadline, lease: lifetime)
        }
      } onCancel: {
        context.closeTransport()
      }
      expiry.cancel()
      await expiry.value
      if !download { await lifetime.release() }
      return response
    } catch {
      expiry.cancel()
      await expiry.value
      await lifetime.release()
      if error is CancellationError { throw error }
      if error is SocialDraftDeadline.Failure || AppViewProxyFailure(error) == .timeout { return failure(.gatewayTimeout, "Upstream request timed out") }
      if error is NIOTooManyBytesError { return failure(.contentTooLarge, "Request body exceeds allowed size") }
      if let http = error as? HTTPError { return failure(http.status, http.body ?? "Private draft request rejected") }
      return failure(.badGateway, "Private draft service unavailable")
    }
  }

  private func forward(request: Request, context: GatewayRequestContext, path: String, method: String, upload: Bool, download: Bool, deadline: ContinuousClock.Instant, lease: PrivateDraftLease) async throws -> Response {
    guard let auth = context.authContext else { throw HTTPError(.unauthorized) }
    let target = GatewayInternalTrust.canonicalPathWithQuery(path: path, query: request.uri.query)
    var upstream = HTTPClientRequest(url: baseURL.trimmingCharacters(in: CharacterSet(charactersIn: "/")) + target)
    upstream.method = method == "GET" ? .GET : .POST
    upstream.headers.add(name: "Authorization", value: auth.authorizationForwardingValue)
    upstream.headers.add(name: "X-Request-ID", value: context.requestId)
    upstream.headers.add(name: "traceparent", value: context.traceContext.traceparent)
    if let proof = auth.dpopProof { upstream.headers.add(name: "DPoP", value: proof) }
    if let proof = auth.upstreamDpopProof { upstream.headers.add(name: ATProtoUpstreamDPoP.headerName, value: proof) }
    if let host = request.head.authority { upstream.headers.add(name: "X-Forwarded-Host", value: host) }
    let forwarded = HTTPField.Name("X-Forwarded-Proto")!
    let scheme = request.headers[forwarded]?.split(separator: ",").first.map(String.init)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "https"
    upstream.headers.add(name: "X-Forwarded-Proto", value: scheme)
    if let internalSecret {
      for header in try GatewayInternalTrust.signedHeaders(secret: internalSecret, did: auth.did, method: method, pathWithQuery: path) {
        upstream.headers.add(name: header.name, value: header.value)
      }
    }
    if upload {
      guard let rawLength = request.headers[.contentLength] else { throw HTTPError(.lengthRequired) }
      guard let length = Int64(rawLength), length >= 0 else { throw HTTPError(.badRequest) }
      guard length <= 100 << 20 else { throw HTTPError(.contentTooLarge) }
      let uploadID = HTTPField.Name("X-Social-Draft-Upload-Id")!
      guard length > 0, let contentType = request.headers[.contentType], let media = MediaType(from: contentType),
        media.type == .application, media.subType.lowercased() == "octet-stream",
        let id = request.headers[uploadID], !id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
      else { throw HTTPError(.badRequest) }
      upstream.headers.add(name: "Content-Type", value: contentType)
      upstream.headers.add(name: "X-Social-Draft-Upload-Id", value: id)
      upstream.body = .stream(request.body, length: .known(length))
    } else if method == "POST" {
      let bytes = try await request.body.collect(upTo: path.contains(".articleDraft.") ? 2 << 20 : 4 << 20)
      upstream.body = .bytes(bytes)
      upstream.headers.add(name: "Content-Type", value: "application/json")
    }
    if !download { upstream.headers.add(name: "Accept", value: "application/json") }
    let reply = try await httpClient.execute(upstream, timeout: .seconds(upload || download ? 120 : 60))
    var headers: HTTPFields = [.cacheControl: "private, no-store", .xContentTypeOptions: "nosniff"]
    for name in ["Content-Type", "Content-Length", "Retry-After", "X-Request-ID", "traceparent"] {
      if let value = reply.headers.first(name: name), let field = HTTPField.Name(name) { headers[field] = value }
    }
    let status = HTTPResponse.Status(code: Int(reply.status.code))
    if download {
      return Response(status: status, headers: headers, body: ResponseBody { writer in
        // ResponseBodyWriter is an inout, non-Sendable value. Keep it on this
        // task and interrupt blocked writes through the request's NIO channel.
        let expiry = Task {
          do {
            try await ContinuousClock().sleep(until: deadline)
            context.closeTransport()
          } catch { /* Successful completion cancels this deadline monitor. */ }
        }
        do {
          try await withTaskCancellationHandler {
            for try await bytes in reply.body { try await writer.write(bytes) }
            try Task.checkCancellation()
            guard ContinuousClock.now < deadline else { throw SocialDraftDeadline.Failure.expired }
            try await writer.finish(nil)
          } onCancel: {
            context.closeTransport()
          }
          expiry.cancel()
          await expiry.value
          await lease.release()
        } catch {
          expiry.cancel()
          await expiry.value
          await lease.release()
          throw error
        }
      })
    }
    return Response(status: status, headers: headers, body: .init(byteBuffer: try await reply.body.collect(upTo: path.contains(".articleDraft.") ? 24 << 20 : 8 << 20)))
  }

  private func failure(_ status: HTTPResponse.Status, _ message: String) -> Response {
    let data = (try? JSONSerialization.data(withJSONObject: ["error": message])) ?? Data()
    return Response(status: status, headers: [.cacheControl: "private, no-store", .contentType: "application/json", .xContentTypeOptions: "nosniff"], body: .init(byteBuffer: ByteBuffer(bytes: data)))
  }
}
