#if DEBUG
import Foundation

/// An ephemeral session owns the fixture protocol; no live service request can escape it.
final class NewsShellUITestTransport: URLProtocol, @unchecked Sendable {
    static func session() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [NewsShellUITestTransport.self]
        return URLSession(configuration: configuration)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        do {
            guard let url = request.url else { throw URLError(.badURL) }
            let body = try Self.body(for: url)
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: body)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}

    private static func body(for url: URL) throws -> Data {
        // Unexpected requests fail locally rather than silently reaching a hosted environment.
        throw URLError(.unsupportedURL)
    }
}
#endif
