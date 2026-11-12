import Foundation

public enum HTTPClientError: Error, Equatable, Sendable {
    case invalidURL(String)
    case invalidResponse
    case badStatus(Int, String, [String: String])
}

struct OAuthErrorResponse: Decodable {
    let error: String
}
