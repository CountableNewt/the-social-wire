import Foundation

/// Deliberately ignores cancellation so tests can release responses from a previous login.
actor SavedLinkPreviewTransport {
    private var requests: [URLRequest] = []
    private var pending: [Int: CheckedContinuation<(Data, URLResponse), Error>] = [:]

    var requestCount: Int { requests.count }

    func send(_ request: URLRequest) async throws -> (Data, URLResponse) {
        let index = requests.count
        requests.append(request)
        return try await withCheckedThrowingContinuation { pending[index] = $0 }
    }

    func complete(_ index: Int, title: String) throws {
        guard let continuation = pending.removeValue(forKey: index) else { return }
        let body = try JSONSerialization.data(withJSONObject: [
            "entryId": "at://did:plc:author/site.standard.document/story", "title": title,
            "publishedAt": "2026-11-05T00:00:00Z"
        ])
        continuation.resume(returning: (body, HTTPURLResponse(url: requests[index].url!, statusCode: 200,
                                                            httpVersion: nil, headerFields: nil)!))
    }
}
