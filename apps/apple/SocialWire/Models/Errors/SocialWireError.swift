import Foundation

enum SocialWireError: LocalizedError {
    case notAuthenticated
    case badResponse(String)
    case invalidURL
    case invalidATURI
    case unsupported

    var errorDescription: String? {
        switch self {
        case .notAuthenticated: "Sign in to continue."
        case .badResponse(let message): message
        case .invalidURL: "The URL is invalid."
        case .invalidATURI: "The AT-URI is invalid."
        case .unsupported: "This action is not supported yet."
        }
    }
}
