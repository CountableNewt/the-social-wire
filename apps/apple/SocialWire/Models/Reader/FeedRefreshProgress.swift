import Foundation

/// Explicit feed refresh progress, one step per publication the server checks.
struct FeedRefreshProgress: Equatable, Sendable {
    var completed = 0
    var total = 0

    /// Title Case status line for the feed footer.
    var label: String {
        guard total > 0 else { return "Checking Publications…" }
        if completed >= total { return "Updating Articles…" }
        return "Checking \(completed) of \(total) \(total == 1 ? "Publication" : "Publications")…"
    }

    var fraction: Double? {
        total > 0 ? min(1, Double(completed) / Double(total)) : nil
    }
}
