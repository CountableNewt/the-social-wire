import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SkejKit

struct EmbeddedLinkPreviewHTTPClient: LinkPreviewHTTPClient {
    let transport: @Sendable (URLRequest) async throws -> (Data, HTTPURLResponse)

    func fetch(url: URL, accept: String, maxBytes: Int) async throws -> LinkPreviewHTTPResponse {
        guard url.scheme?.lowercased() == "https", url.host != nil, url.user == nil, url.password == nil else {
            throw LinkPreviewError.unsafeURL
        }
        var request = URLRequest(url: url)
        request.setValue(accept, forHTTPHeaderField: "Accept")
        let (body, response) = try await transport(request)
        guard (200..<300).contains(response.statusCode) else { throw LinkPreviewError.badStatus(response.statusCode) }
        guard body.count <= maxBytes else { throw LinkPreviewError.responseTooLarge }
        guard let contentType = response.value(forHTTPHeaderField: "Content-Type")?.split(separator: ";").first,
              !contentType.isEmpty else { throw LinkPreviewError.unsupportedContentType }
        return LinkPreviewHTTPResponse(body: body, mimeType: String(contentType), finalURL: url)
    }
}
