import Foundation

struct RepoRecord<Value: Codable & Sendable>: Codable, Identifiable, Sendable {
    let uri: String
    let cid: String?
    let value: Value

    var id: String { uri }
}
