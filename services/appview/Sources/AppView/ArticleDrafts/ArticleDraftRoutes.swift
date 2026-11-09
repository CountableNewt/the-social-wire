import Foundation
import GatewayCore
import Hummingbird
import HTTPTypes
import NIOFoundationCompat

struct ArticleDraftRoutes: Sendable {
  let service: (any SocialDraftStoring)?

  func register(on group: RouterGroup<GatewayRequestContext>) {
    for (name, path) in [("list", "/xrpc/app.thesocialwire.articleDraft.list"), ("get", "/xrpc/app.thesocialwire.articleDraft.get")] {
      group.get(RouterPath(path)) { request, context in try await handle(name, request: request, context: context) }
    }
    for (name, path) in [("put", "/xrpc/app.thesocialwire.articleDraft.put"), ("delete", "/xrpc/app.thesocialwire.articleDraft.delete")] {
      group.post(RouterPath(path)) { request, context in try await handle(name, request: request, context: context) }
    }
  }

  func handle(_ name: String, request: Request, context: GatewayRequestContext) async throws -> Response {
    let deadline = SocialDraftDeadline(seconds: 60, closeTransport: context.closeTransport)
    let work = Task { try await operation(name, request: request, context: context) }
    deadline.bind { work.cancel() }
    let response: Response
    do {
      response = try await withTaskCancellationHandler { try await work.value } onCancel: {
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
    case "list":
      let raw = request.uri.queryParameters.get("limit")
      guard raw == nil || Int(raw!) != nil else { throw SocialDraftStorageError.invalid }
      return try Self.json(try await service.articleDrafts(owner: owner, cursor: request.uri.queryParameters.get("cursor"), limit: raw.flatMap(Int.init) ?? 20))
    case "get":
      guard let id = request.uri.queryParameters.get("draftId") else { throw SocialDraftStorageError.invalid }
      return try Self.json(try await service.articleDraft(owner: owner, id: id))
    case "put":
      let input: ArticleDraftPutInput = try await Self.decode(request, fields: ["expectedRevision", "mutationId", "draft", "attachments"])
      return try Self.json(try await service.putArticleDraft(owner: owner, input: input))
    case "delete":
      let input: ArticleDraftDeleteInput = try await Self.decode(request, fields: ["draftId", "expectedRevision", "mutationId"])
      return try Self.json(try await service.deleteArticleDraft(owner: owner, input: input))
    default: throw SocialDraftStorageError.invalid
    }
  }

  private static func decode<T: Decodable>(_ request: Request, fields: Set<String>) async throws -> T {
    guard let raw = request.headers[.contentType], let media = MediaType(from: raw),
      media.type == .application, media.subType.lowercased() == "json" else { throw SocialDraftStorageError.invalid }
    let bytes: ByteBuffer
    do { bytes = try await request.body.collect(upTo: 2 << 20) }
    catch is CancellationError { throw CancellationError() }
    catch { throw SocialDraftStorageError.quota }
    let data = Data(buffer: bytes)
    guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
      Set(object.keys) == fields, !object.values.contains(where: { $0 is NSNull }) else { throw SocialDraftStorageError.invalid }
    if fields.contains("draft") {
      let allowed: Set<String> = ["id", "title", "markdown", "excerpt", "path", "tags", "publicationUri", "createdAt", "updatedAt", "assets", "coverAssetId", "publishedUri", "scheduleRecordKey", "scheduleUri", "publishingRecordKey", "publicationClaimId"]
      let assetFields: Set<String> = ["id", "alt", "width", "height", "mimeType", "name"]
      guard let draft = object["draft"] as? [String: Any], Set(draft.keys).isSubset(of: allowed),
        !draft.values.contains(where: { $0 is NSNull }), let assets = draft["assets"] as? [[String: Any]],
        assets.allSatisfy({ Set($0.keys) == assetFields && !$0.values.contains(where: { $0 is NSNull }) }),
        let refs = object["attachments"] as? [[String: Any]],
        refs.allSatisfy({ Set($0.keys) == ["localAssetId", "assetId"] && !$0.values.contains(where: { $0 is NSNull }) })
      else { throw SocialDraftStorageError.invalid }
    }
    do { return try JSONDecoder().decode(T.self, from: data) }
    catch { throw SocialDraftStorageError.invalid }
  }

  private static func json<T: Encodable>(_ value: T, status: HTTPResponse.Status = .ok) throws -> Response {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.withoutEscapingSlashes]
    return Response(status: status, headers: [.contentType: "application/json"], body: .init(byteBuffer: .init(data: try encoder.encode(value))))
  }

  private static func failure(_ error: Error) throws -> Response {
    let status: HTTPResponse.Status, code: String, message: String
    switch error as? SocialDraftStorageError {
    case .invalid: (status, code, message) = (.badRequest, "InvalidRequest", "Invalid private article draft request")
    case .notFound: (status, code, message) = (.notFound, "NotFound", "Private article draft not found")
    case .conflict, .assetRetired: (status, code, message) = (.conflict, "Conflict", "Private draft revision conflict")
    case .quota: (status, code, message) = (.contentTooLarge, "Quota", "Private article draft quota exceeded")
    case .uploadPending: (status, code, message) = (.conflict, "UploadPending", "Private draft attachment upload pending")
    default: (status, code, message) = (.serviceUnavailable, "Unavailable", "Private article draft storage unavailable")
    }
    return try json(["error": code, "message": message], status: status)
  }
}
