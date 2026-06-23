import Foundation

enum ReaderListSourceStorage {
    static let userDefaultsKey = "the-social-wire.reader-list-source.v1"

    static func load() -> ReaderListSource {
        guard let raw = UserDefaults.standard.string(forKey: userDefaultsKey),
              let source = ReaderListSource(rawValue: raw) else {
            return .subscribed
        }
        return source
    }

    static func save(_ source: ReaderListSource) {
        UserDefaults.standard.set(source.rawValue, forKey: userDefaultsKey)
    }
}
