import Foundation

struct PARResponse: Decodable, Sendable {
    let requestURI: String
    let expiresIn: Int?

    enum CodingKeys: String, CodingKey {
        case requestURI = "request_uri"
        case expiresIn = "expires_in"
    }
}
