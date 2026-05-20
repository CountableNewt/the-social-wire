import Foundation

struct GenericListRecordsResponse: Codable, Sendable {
    let records: [GenericRepoRecord]
    let cursor: String?
}
