import Foundation

struct AppViewEntryListResponse: Codable, Sendable {
    let entries: [EntryListItem]
    let cursor: String?
}
