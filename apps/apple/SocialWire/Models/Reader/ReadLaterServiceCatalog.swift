import Foundation

/// Read-later backends users can designate (mirrors **`READ_LATER_SERVICES`** on web).
enum ReadLaterServiceCatalog {
    /// Same key as **`READ_LATER_SERVICE_STORAGE_KEY`** in `apps/web/src/lib/readLaterServices.ts`.
    static let userDefaultsStorageKey = "social-wire.saved.read-later-service"

    static let defaultServiceId = "latr-link"

    struct Option: Identifiable, Equatable, Sendable {
        let id: String
        let label: String
        /// `true` when Social Wire merges HTTPS saves from the user's PDS (`com.latr.saved.*`).
        let connectedViaPDS: Bool
        let loginLabel: String?
        let loginURL: URL?
    }

    static let latrPdsServiceIds: Set<String> = ["latr-link", "latrkit"]

    static func isLatrPdsReadLaterService(_ id: String) -> Bool {
        latrPdsServiceIds.contains(id)
    }

    static let options: [Option] = [
        Option(
            id: "latr-link",
            label: "L@tr.link",
            connectedViaPDS: true,
            loginLabel: nil,
            loginURL: nil
        ),
        Option(
            id: "latrkit",
            label: "LatrKit",
            connectedViaPDS: true,
            loginLabel: nil,
            loginURL: nil
        ),
        Option(
            id: "instapaper",
            label: "Instapaper",
            connectedViaPDS: false,
            loginLabel: "Log In To Instapaper",
            loginURL: URL(string: "https://www.instapaper.com/user/login")
        ),
        Option(
            id: "omnivore",
            label: "Omnivore",
            connectedViaPDS: false,
            loginLabel: "Log In To Omnivore",
            loginURL: URL(string: "https://omnivore.app/login")
        ),
        Option(
            id: "readwise-reader",
            label: "Readwise Reader",
            connectedViaPDS: false,
            loginLabel: "Log In To Readwise Reader",
            loginURL: URL(string: "https://read.readwise.io/")
        ),
        Option(
            id: "raindrop",
            label: "Raindrop.io",
            connectedViaPDS: false,
            loginLabel: "Log In To Raindrop.io",
            loginURL: URL(string: "https://app.raindrop.io/")
        ),
    ]

    static func isKnownServiceId(_ raw: String) -> Bool {
        options.contains { $0.id == raw }
    }

    static func label(for id: String) -> String {
        options.first { $0.id == id }?.label ?? id
    }
}
