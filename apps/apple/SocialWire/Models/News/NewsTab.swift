import Foundation

/// Stable top-level destinations for the adaptive Apple News-style shell.
enum NewsTab: String, CaseIterable, Codable, Identifiable, Hashable, Sendable {
    case library
    case saved
    case search

    var id: String { rawValue }

    var title: String {
        switch self {
        case .library: "Library"
        case .saved: "Saved"
        case .search: "Search"
        }
    }

    var systemImage: String {
        switch self {
        case .library: "books.vertical"
        case .saved: "bookmark"
        case .search: "magnifyingglass"
        }
    }

    static let available: [NewsTab] = allCases

    static func defaultTab(in availableTabs: [NewsTab]) -> NewsTab {
        if availableTabs.contains(.library) { return .library }
        return availableTabs.first ?? .library
    }
}
