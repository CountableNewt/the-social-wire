import Foundation
import GatewayCore
import Hummingbird
import HTTPTypes
import NIOFoundationCompat

struct SocialDraftRoutes: Sendable {
  let service: SocialDraftService?
  var transferSeconds: Double = 120
  var mutationSeconds: Double = 60
  private static let prefix = "/xrpc/app.thesocialwire.socialDraft."

  func register(on group: RouterGroup<GatewayRequestContext>) {
    for name in ["reserveAttachment", "uploadAttachment", "putManifest", "deleteManifest"] {
      group.post(RouterPath(Self.prefix + name)) { request, context in try await handle(name, request: request, context: context) }
    }
    for name in ["getAttachment", "getManifest"] {
      group.get(RouterPath(Self.prefix + name)) { request, context in try await handle(name, request: request, context: context) }
    }
  }

  func handle(_ name: String, request: Request, context: GatewayRequestContext) async throws -> Response {
    let deadline = SocialDraftDeadline(seconds: ["uploadAttachment", "getAttachment"].contains(name) ? transferSeconds : mutationSeconds,
      closeTransport: context.closeTransport)
    let work = Task { try await operation(name, request: request, context: context) }
    deadline.bind { work.cancel() }
    let response: Response
    do {
      response = try await withTaskCancellationHandler {
        try await work.value
      } onCancel: {
        work.cancel()
        context.closeTransport()
      }
    } catch { response = try Self.failure(error) }
    var bounded = response
    let original = response.body
    bounded.body = ResponseBody(contentLength: original.contentLength) { writer in
      defer { deadline.complete() }
      try await original.write(writer)
    }
    bounded.headers[.cacheControl] = "private, no-store"
    bounded.headers[HTTPField.Name("X-Content-Type-Options")!] = "nosniff"
    return bounded
  }

  private func operation(_ name: String, request: Request, context: GatewayRequestContext) async throws -> Response {
    guard let owner = context.authContext?.did, !owner.isEmpty, owner != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID else {
      return try Self.json(["error": "Unauthorized", "message": "Authentication required"], status: .unauthorized)
    }
    guard let service else { throw SocialDraftStorageError.unavailable }
    switch name {
    case "reserveAttachment":
      let body: SocialDraftReserveInput = try await Self.decode(request, fields: ["assetId", "name", "contentType", "byteLength", "sha256", "kind"])
      return try Self.json(["attachment": try await service.reserve(owner: owner, input: body)])
    case "uploadAttachment":
      guard let raw = request.headers[.contentLength], let length = Int64(raw), length >= 0 else {
        return try Self.json(["error": "LengthRequired", "message": "Content-Length required"], status: .lengthRequired)
      }
      guard length <= SocialDraftValidation.maximumFileBytes else { throw SocialDraftStorageError.quota }
      guard length > 0, let id = request.headers[HTTPField.Name("X-Social-Draft-Upload-Id")!], !id.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
        Self.mediaType(request) == "application/octet-stream" else { throw SocialDraftStorageError.invalid }
      let asset = try await service.upload(owner: owner, id: id, body: request.body, contentLength: length)
      return try Self.json(["attachment": asset])
    case "getAttachment":
      guard let id = request.uri.queryParameters.get("assetId"), !id.isEmpty else { throw SocialDraftStorageError.invalid }
      let download = try await service.download(owner: owner, id: id)
      return Response(status: .ok, headers: [.contentType: download.asset.contentType],
        body: ResponseBody(contentLength: Int(download.asset.byteLength)) { writer in
          defer { withExtendedLifetime(download) {} }
          for try await buffer in SocialDraftFileSequence(file: download.file) { try await writer.write(buffer) }
          try await writer.finish(nil)
        })
    case "getManifest":
      guard let id = request.uri.queryParameters.get("draftId"), !id.isEmpty else { throw SocialDraftStorageError.invalid }
      return try Self.json(try await service.manifest(owner: owner, id: id))
    case "putManifest":
      let body: SocialDraftPutManifestInput = try await Self.decode(request, fields: ["draftId", "expectedRevision", "attachments"])
      return try Self.json(try await service.putManifest(owner: owner, input: body))
    case "deleteManifest":
      let body: SocialDraftDeleteManifestInput = try await Self.decode(request, fields: ["draftId", "expectedRevision"])
      return try Self.json(try await service.putManifest(owner: owner,
        input: .init(draftId: body.draftId, expectedRevision: body.expectedRevision, attachments: []), deleted: true))
    default: throw SocialDraftStorageError.invalid
    }
  }

  private static func mediaType(_ request: Request) -> String {
    guard let raw = request.headers[.contentType], let parsed = MediaType(from: raw) else { return "" }
    return "\(parsed.type.rawValue)/\(parsed.subType)"
  }

  private static func decode<T: Decodable>(_ request: Request, fields: Set<String>) async throws -> T {
    guard mediaType(request) == "application/json" else { throw SocialDraftStorageError.invalid }
    let bytes: ByteBuffer
    do { bytes = try await request.body.collect(upTo: 1 << 20) }
    catch is CancellationError { throw CancellationError() }
    catch { throw SocialDraftStorageError.invalid }
    let data = Data(buffer: bytes)
    guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
      Set(object.keys) == fields, !object.values.contains(where: { $0 is NSNull }) else { throw SocialDraftStorageError.invalid }
    do { return try JSONDecoder().decode(T.self, from: data) }
    catch { throw SocialDraftStorageError.invalid }
  }

  private static func json<T: Encodable>(_ value: T, status: HTTPResponse.Status = .ok) throws -> Response {
    Response(status: status, headers: [.contentType: "application/json"],
      body: .init(byteBuffer: .init(data: try JSONEncoder().encode(value))))
  }

  private static func failure(_ error: Error) throws -> Response {
    let status: HTTPResponse.Status
    let code: String
    let message: String
    switch error as? SocialDraftStorageError {
    case .invalid: (status, code, message) = (.badRequest, "InvalidRequest", "Invalid private draft request")
    case .notFound: (status, code, message) = (.notFound, "NotFound", "Private draft attachment or manifest not found")
    case .conflict: (status, code, message) = (.conflict, "Conflict", "Private draft revision conflict")
    case .assetRetired: (status, code, message) = (.conflict, "AssetRetired", "Private draft attachment identifier retired")
    case .quota: (status, code, message) = (.contentTooLarge, "Quota", "Private draft quota exceeded")
    case .uploadPending: (status, code, message) = (.conflict, "UploadPending", "Private draft attachment upload pending")
    default: (status, code, message) = (.serviceUnavailable, "Unavailable", "Private draft storage unavailable")
    }
    return try json(["error": code, "message": message], status: status)
  }
}
