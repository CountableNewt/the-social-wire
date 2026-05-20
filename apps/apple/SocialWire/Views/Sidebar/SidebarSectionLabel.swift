import SwiftUI

struct SidebarSectionLabel: View {
    let title: String
    let unreadCount: Int

    var body: some View {
        HStack {
            Text(title)
                .font(.subheadline.weight(.medium))
            Spacer(minLength: 6)
            SidebarCountLabel(count: unreadCount)
        }
    }
}
