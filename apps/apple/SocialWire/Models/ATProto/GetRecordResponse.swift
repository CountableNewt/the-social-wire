import Foundation

struct GetRecordResponse<Value: Codable & Sendable>: Codable, Sendable {
    let uri: String
    let cid: String?
    let value: Value
}
