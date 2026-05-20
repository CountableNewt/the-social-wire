import Foundation

struct GatewayHTTPResult: Sendable {
    let statusCode: Int
    let etagHeader: String?
    let body: Data
}
