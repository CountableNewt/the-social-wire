import Foundation

struct AppViewReadMarkBody: Encodable, Sendable {
    let subjectUri: String
    let readAt: String
}

struct AppViewReadMarkDeleteBody: Encodable, Sendable {
    let subjectUri: String
}

struct AppViewEnrollBody: Encodable, Sendable {
    let authorDids: [String]
}
