import GatewayCore
import HTTPTypes
import Hummingbird

/// Carries the old edge's private-draft admission and Pro checks into direct
/// AppView routes. Transfer leases last until the response body finishes.
struct StandaloneAdmissionMiddleware: RouterMiddleware {
  typealias Context = GatewayRequestContext
  let runtime: StandaloneEdgeRuntime

  func handle(
    _ request: Request, context: GatewayRequestContext,
    next: (Request, GatewayRequestContext) async throws -> Response
  ) async throws -> Response {
    let path = request.uri.path
    if ["/v1/analytics/summary", "/v1/analytics/refresh", "/v1/analytics/bluesky"].contains(path),
      runtime.configuration.entitlements.gatingEnabled,
      !runtime.configuration.entitlements.hasPro(did: context.authContext?.did ?? "")
    {
      return Response(
        status: .forbidden,
        headers: [.contentType: "application/json", .cacheControl: "private, no-store"],
        body: .init(
          byteBuffer: .init(
            string: #"{"error":"ProRequired","message":"A Pro subscription is required"}"#)))
    }
    guard
      path.hasPrefix("/xrpc/app.thesocialwire.articleDraft.")
        || path.hasPrefix("/xrpc/app.thesocialwire.socialDraft."),
      !path.hasSuffix(".getGifFile")
    else { return try await next(request, context) }
    guard let did = context.authContext?.did, !did.isEmpty else { throw HTTPError(.unauthorized) }
    let transfer = path.hasSuffix(".uploadAttachment") || path.hasSuffix(".getAttachment")
    let lease: EdgeDraftAdmission.Lease
    do { lease = try await runtime.admission.acquire(owner: did, transfer: transfer) } catch let
      error as EdgeDraftAdmission.Failure
    {
      return Response(
        status: error == .limited ? .tooManyRequests : .serviceUnavailable,
        headers: [.retryAfter: "1", .cacheControl: "private, no-store"])
    }
    let lifetime = PrivateDraftLease(admission: runtime.admission, lease: lease)
    do {
      var response = try await next(request, context)
      if transfer {
        let body = response.body
        response.body = ResponseBody(contentLength: body.contentLength) { writer in
          do {
            try await body.write(writer)
            await lifetime.release()
          } catch {
            await lifetime.release()
            throw error
          }
        }
      } else {
        await lifetime.release()
      }
      return response
    } catch {
      await lifetime.release()
      throw error
    }
  }
}
