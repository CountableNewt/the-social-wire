import SwiftUI

struct SidebarCountLabel: View {
    let count: Int
    private let accessibilityDescription: String

    init(count: Int, accessibilityDescription: String = "unread") {
        self.count = count
        self.accessibilityDescription = accessibilityDescription
    }

    var body: some View {
        if count > 0 {
            Text("\(count)")
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
                .accessibilityLabel("\(count) \(accessibilityDescription)")
        }
    }
}
