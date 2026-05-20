import Foundation

/// Scope for the reader shell mark-read toolbar action (pane / selection aware).
enum ReaderMarkReadScope: Equatable {
    case allLists
    case list(ReaderListSource)
    case publication(publicationId: String)
    case entry(entryId: String)
    case unavailable
}
