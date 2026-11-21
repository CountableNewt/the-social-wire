import Foundation

/// One line of `app.thesocialwire.appview.refreshFeed`, keyed by `kind`.
enum FeedRefreshStreamEvent: Decodable, Sendable, Equatable {
    case started(total: Int)
    case publication(Publication)
    case done(total: Int, indexed: Int, unavailable: Int)
    /// Unknown kinds are skipped so the server can add events.
    case unknown

    struct Publication: Decodable, Sendable, Equatable {
        enum Status: String, Decodable, Sendable {
            case updated, checked, unavailable
        }

        let publicationId: String
        let status: Status
        let indexed: Int
        let completed: Int
        let total: Int
    }

    private struct Started: Decodable { let total: Int }
    private struct Done: Decodable { let total: Int; let indexed: Int; let unavailable: Int }

    private enum CodingKeys: String, CodingKey {
        case kind, started, publication, done
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        switch try container.decode(String.self, forKey: .kind) {
        case "started":
            self = .started(total: try container.decode(Started.self, forKey: .started).total)
        case "publication":
            self = .publication(try container.decode(Publication.self, forKey: .publication))
        case "done":
            let done = try container.decode(Done.self, forKey: .done)
            self = .done(total: done.total, indexed: done.indexed, unavailable: done.unavailable)
        default:
            self = .unknown
        }
    }

    /// Delivers each event in order and returns once `done` arrives.
    @MainActor
    static func consume<Lines: AsyncSequence>(
        _ lines: Lines,
        onEvent: (FeedRefreshStreamEvent) async -> Void
    ) async throws where Lines.Element == String {
        for try await line in lines {
            try Task.checkCancellation()
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !trimmed.isEmpty else { continue }
            let event = try JSONDecoder().decode(Self.self, from: Data(trimmed.utf8))
            if event == .unknown { continue }
            await onEvent(event)
            if case .done = event { return }
        }
        throw SocialWireError.badResponse("Refresh was interrupted. Please try again.")
    }
}
