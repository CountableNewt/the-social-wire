import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import SkejKit

struct SchedulingHTTPClient: HTTPClient {
    let transport: @Sendable (URLRequest) async throws -> (Data, HTTPURLResponse)

    func data(_ request: HTTPRequest) async throws -> HTTPResponseData {
        guard let url = URL(string: request.url),
              url.scheme == "https", url.user == nil, url.password == nil,
              url.host != nil, url.fragment == nil else {
            throw HTTPClientError.invalidURL("Invalid scheduling endpoint")
        }
        var outgoing = URLRequest(url: url)
        outgoing.httpMethod = request.method
        outgoing.httpBody = request.body
        for (name, value) in request.headers { outgoing.setValue(value, forHTTPHeaderField: name) }
        let (body, http) = try await transport(outgoing)
        var headers: [String: String] = [:]
        for name in ["dpop-nonce", "retry-after"] {
            if let value = http.value(forHTTPHeaderField: name), value.utf8.count <= 2048 {
                headers[name] = value
            }
        }
        guard (200..<300).contains(http.statusCode) else {
            throw HTTPClientError.badStatus(http.statusCode, sanitizedErrorCode(body), headers)
        }
        return HTTPResponseData(body: body, headers: headers, statusCode: http.statusCode)
    }
}

private func sanitizedErrorCode(_ data: Data) -> String {
    guard data.count <= 64 * 1024,
          let error = try? JSONDecoder().decode(UpstreamErrorCode.self, from: data),
          [
            "invalid_grant", "invalid_client", "invalid_client_metadata", "invalid_scope", "use_dpop_nonce",
            "RecordNotFound", "InvalidRequest", "ExpiredToken", "AuthenticationRequired", "InvalidToken",
          ].contains(error.error)
    else { return "" }
    return #"{"error":"\#(error.error)"}"#
}

private struct UpstreamErrorCode: Decodable {
    let error: String
}
