import Foundation

enum SidebarSelection: Hashable {
    case saved
    /// Opened from the footer profile row (not a sidebar list tag).
    case myPublications
    case publication(String)
}
