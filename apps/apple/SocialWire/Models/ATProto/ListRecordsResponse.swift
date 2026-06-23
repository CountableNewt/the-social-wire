import Foundation

struct ListRecordsResponse<Value: Codable & Sendable>: Codable, Sendable {
    let records: [RepoRecord<Value>]
    let cursor: String?
}
