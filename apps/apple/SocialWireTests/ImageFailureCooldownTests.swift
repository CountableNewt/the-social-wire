import Foundation
import Testing
@testable import SocialWire

@Suite("Image failure cooldown")
struct ImageFailureCooldownTests {
    private actor Downloads {
        var count = 0

        func response(for url: URL, status: Int) throws -> (URL, URLResponse) {
            count += 1
            let file = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
            try Data().write(to: file)
            let response = try #require(HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: ["Content-Type": "image/x-icon"]))
            return (file, response)
        }
    }

    @Test("empty images and HTTP failures suppress repeat downloads", arguments: [200, 404])
    func suppressesFailures(status: Int) async throws {
        let downloads = Downloads()
        let cache = ImageCacheService { url in try await downloads.response(for: url, status: status) }
        let url = try #require(URL(string: "https://example.com/favicon.ico"))
        #expect(await cache.image(for: url, maxPixelSize: 32) == nil)
        #expect(await cache.image(for: url, maxPixelSize: 32) == nil)
        #expect(await downloads.count == 1)
    }

    @Test("expired failures allow retry")
    func retriesAfterExpiry() async throws {
        let downloads = Downloads()
        let cache = ImageCacheService(failureCooldown: 0) { url in try await downloads.response(for: url, status: 404) }
        let url = try #require(URL(string: "https://example.com/favicon.ico"))
        _ = await cache.image(for: url, maxPixelSize: 32)
        _ = await cache.image(for: url, maxPixelSize: 32)
        #expect(await downloads.count == 2)
    }
}
