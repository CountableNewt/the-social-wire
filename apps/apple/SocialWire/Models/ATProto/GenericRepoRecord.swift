import Foundation

struct GenericRepoRecord: Codable, Identifiable, Sendable {
    let uri: String
    let cid: String?
    let value: JSONValue

    var id: String { uri }
}
