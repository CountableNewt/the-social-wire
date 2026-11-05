import AsyncHTTPClient
import Foundation
import GatewayCore
import HTTPTypes
import Hummingbird
import NIOCore

struct GIFRoutes {
  let catalog: GIFCatalogService
  let limiter: WireRequestLimiter
  let admission: SocialDraftAdmission

  func register(on group: RouterGroup<GatewayRequestContext>) {
    for path in ["/v1/media/gifs", "/v1/media/gifs/thumbnail"] {
      group.get(RouterPath(path)) { request, context in
        guard let auth = context.authContext, !auth.did.isEmpty,
          auth.did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
        else { return error(.unauthorized, "Authentication required") }
        guard await limiter.consume(key: auth.did, audience: .authenticated) else { return error(.tooManyRequests, "Too many discovery requests") }
        do {
          return try await SocialDraftDeadline.run(until: .now.advanced(by: .seconds(15))) {
            try await search(request, thumbnail: path.hasSuffix("thumbnail"))
          }
        } catch let failure as HTTPError { return error(failure.status, failure.body ?? "GIF request unavailable") }
        catch { return self.error(.badGateway, "GIF service unavailable") }
      }
    }
    group.get("/xrpc/app.thesocialwire.socialDraft.getGifFile") { request, context in
      try await privateFile(request, context: context)
    }
  }

  private func query(_ request: Request) -> [URLQueryItem] {
    URLComponents(string: "https://query.invalid/?\((request.uri.query ?? "").replacingOccurrences(of: "+", with: "%20"))")?.queryItems ?? []
  }

  private func search(_ request: Request, thumbnail: Bool) async throws -> Response {
    guard !catalog.apiKey.isEmpty else { throw HTTPError(.serviceUnavailable) }
    let query = query(request)
    let id = query.first { $0.name == "id" }?.value ?? ""
    let text = (query.first { $0.name == "q" }?.value ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
    let cursor = query.first { $0.name == "cursor" }?.value ?? ""
    guard text.utf8.count <= 200, cursor.utf8.count <= 512, id.isEmpty || GIFMediaPolicy.validID(id) else { throw HTTPError(.badRequest) }
    var endpoint = text.isEmpty ? "featured" : "search"
    var parameters = text.isEmpty ? [] : [URLQueryItem(name: "q", value: text)]
    if !cursor.isEmpty { parameters.append(URLQueryItem(name: "pos", value: cursor)) }
    if !id.isEmpty { endpoint = "posts"; parameters = [URLQueryItem(name: "ids", value: id)] }
    let page = try await catalog.fetch(endpoint: endpoint, parameters: parameters)
    let assets = id.isEmpty ? page.results : page.results.filter { $0.id == id }
    if !id.isEmpty && assets.isEmpty { throw HTTPError(.notFound) }
    if thumbnail {
      guard !id.isEmpty else { throw HTTPError(.badRequest) }
      let asset = assets[0]
      let exchange = try await PublicMediaFetcher.open(url: asset.poster, httpClient: catalog.httpClient, timeout: .seconds(15))
      do {
        let mime = exchange.response.headers.first(name: "Content-Type")?.split(separator: ";").first.map(String.init) ?? ""
        guard exchange.response.status.code == 200, GIFMediaPolicy.allowedMIME(mime, rendition: "poster") else { throw HTTPError(.badGateway) }
        let data = try await exchange.response.body.collect(upTo: 1_000_000)
        guard data.readableBytes > 0 else { throw HTTPError(.badGateway) }
        try await exchange.client.shutdown()
        return Response(status: .ok, headers: [.contentType: mime, .cacheControl: "no-store"], body: .init(byteBuffer: data))
      } catch { try? await exchange.client.shutdown(); throw error }
    }
    return Response(status: .ok, headers: [.contentType: "application/json", .cacheControl: "no-store"], body: .init(byteBuffer: ByteBuffer(bytes: try JSONEncoder().encode(GIFCatalogPage(results: assets, next: page.next)))))
  }

  private func privateFile(_ request: Request, context: GatewayRequestContext) async throws -> Response {
    guard let auth = context.authContext, !auth.did.isEmpty,
      auth.did != GatewayInternalTrustAuthMiddleware.anonymousDiscoveryDID
    else { return error(.unauthorized, "Authentication required", privateResponse: true) }
    let lease: SocialDraftAdmission.Lease
    do { lease = try await admission.acquire(owner: auth.did, transfer: true) }
    catch let failure as SocialDraftAdmission.Failure {
      var response = error(failure == .limited ? .tooManyRequests : .serviceUnavailable, "GIF transfer unavailable", privateResponse: true)
      response.headers[.retryAfter] = "1"
      return response
    }
    let lifetime = PrivateDraftLease(admission: admission, lease: lease)
    let query = query(request)
    let ids = query.filter { $0.name == "id" }
    let renditions = query.filter { $0.name == "rendition" }
    guard query.count == 2, ids.count == 1, renditions.count == 1, let id = ids[0].value,
      GIFMediaPolicy.validID(id), let rendition = renditions[0].value,
      ["gif", "poster", "mp4", "webm"].contains(rendition)
    else {
      await lifetime.release()
      return error(.badRequest, "Invalid GIF file selector", privateResponse: true)
    }
    guard !catalog.apiKey.isEmpty else {
      await lifetime.release()
      return error(.serviceUnavailable, "GIF files unavailable", privateResponse: true)
    }
    let deadline = ContinuousClock.now.advanced(by: .seconds(120))
    let expiry = Task { try? await ContinuousClock().sleep(until: deadline); if !Task.isCancelled { context.closeTransport() } }
    do {
      let file = try await SocialDraftDeadline.run(until: deadline) {
        try await download(id: id, rendition: rendition)
      }
      expiry.cancel(); await expiry.value
      return Response(status: .ok, headers: [.contentType: file.mime, .contentLength: String(file.length), .cacheControl: "private, no-store", .xContentTypeOptions: "nosniff"], body: ResponseBody(contentLength: file.length) { writer in
        let expiry = Task { try? await ContinuousClock().sleep(until: deadline); if !Task.isCancelled { context.closeTransport() } }
        do {
          let input = try FileHandle(forReadingFrom: file.url)
          defer { try? input.close() }
          try await withTaskCancellationHandler {
            while let data = try input.read(upToCount: 64 << 10), !data.isEmpty {
              try Task.checkCancellation()
              try await writer.write(ByteBuffer(bytes: data))
            }
            guard ContinuousClock.now < deadline else { throw SocialDraftDeadline.Failure.expired }
            try await writer.finish(nil)
          } onCancel: { context.closeTransport() }
          expiry.cancel(); await expiry.value
          await lifetime.release()
        } catch {
          expiry.cancel(); await expiry.value
          await lifetime.release()
          throw error
        }
      })
    } catch {
      expiry.cancel(); await expiry.value
      await lifetime.release()
      if error is CancellationError { throw error }
      if let failure = error as? HTTPError { return self.error(failure.status, "GIF file unavailable", privateResponse: true) }
      return self.error(.badGateway, "GIF file unavailable", privateResponse: true)
    }
  }

  private func download(id: String, rendition: String) async throws -> PrivateGIFFile {
    let page = try await catalog.fetch(endpoint: "posts", parameters: [URLQueryItem(name: "ids", value: id)])
    guard let asset = page.results.first(where: { $0.id == id }) else { throw HTTPError(.notFound) }
    let raw: String?
    switch rendition { case "poster": raw = asset.poster; case "mp4": raw = asset.mp4; case "webm": raw = asset.webm; default: raw = asset.url }
    guard let raw, !raw.isEmpty else { throw HTTPError(.notFound) }
    guard let url = GIFMediaPolicy.fileURL(raw) else { throw HTTPError(.badGateway) }
    let exchange = try await PublicMediaFetcher.open(url: url.absoluteString, httpClient: catalog.httpClient, timeout: .seconds(120), headers: ["Accept-Encoding": "identity"])
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("social-draft-gif-\(UUID().uuidString)", isDirectory: true)
    do {
      guard exchange.response.status.code == 200,
        let contentType = exchange.response.headers.first(name: "Content-Type"), let media = MediaType(from: contentType)
      else { throw HTTPError(.badGateway) }
      let mime = "\(media.type.rawValue)/\(media.subType)"
      let encoding = exchange.response.headers.first(name: "Content-Encoding") ?? ""
      guard GIFMediaPolicy.allowedMIME(mime, rendition: rendition), encoding.isEmpty || encoding == "identity" else { throw HTTPError(.badGateway) }
      let limit = rendition == "poster" ? 1_000_000 : 100 << 20
      let expected = exchange.response.headers.first(name: "Content-Length").flatMap(Int.init)
      guard expected == nil || expected! <= limit else { throw HTTPError(.contentTooLarge) }
      try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
      let url = directory.appendingPathComponent("media")
      guard FileManager.default.createFile(atPath: url.path, contents: nil, attributes: [.posixPermissions: 0o600]) else { throw HTTPError(.serviceUnavailable) }
      let output = try FileHandle(forWritingTo: url)
      defer { try? output.close() }
      var length = 0
      var head = Data()
      for try await bytes in exchange.response.body {
        try Task.checkCancellation()
        guard bytes.readableBytes <= limit - length else { throw HTTPError(.contentTooLarge) }
        let data = Data(bytes.readableBytesView)
        if head.count < 512 { head.append(data.prefix(512 - head.count)) }
        try output.write(contentsOf: data)
        length += data.count
      }
      guard length > 0, expected == nil || expected == length, GIFMediaPolicy.detectedMIME(head) == mime else { throw HTTPError(.badGateway) }
      try await exchange.client.shutdown()
      return PrivateGIFFile(directory: directory, url: url, length: length, mime: mime)
    } catch {
      try? await exchange.client.shutdown()
      try? FileManager.default.removeItem(at: directory)
      throw error
    }
  }

  private func error(_ status: HTTPResponse.Status, _ message: String, privateResponse: Bool = false) -> Response {
    let data = (try? JSONSerialization.data(withJSONObject: ["error": message])) ?? Data()
    return Response(status: status, headers: [.contentType: "application/json", .cacheControl: privateResponse ? "private, no-store" : "no-store", .xContentTypeOptions: "nosniff"], body: .init(byteBuffer: ByteBuffer(bytes: data)))
  }
}
