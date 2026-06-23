import Foundation

struct DIDDocument: Codable, Sendable {
    struct Service: Codable, Sendable {
        let id: String
        let type: String?
        let serviceEndpoint: String
    }

    let service: [Service]?
}
