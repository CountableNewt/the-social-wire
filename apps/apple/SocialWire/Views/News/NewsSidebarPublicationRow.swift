import SwiftUI

struct NewsSidebarPublicationRow: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let publication: DiscoveredPublication
    let onSelect: (NewsTabSlot) -> Void

    var body: some View {
        Button {
            onSelect(.subscribed)
            Task { await appModel.selectPublication(publication) }
        } label: {
            PublicationSidebarRow(
                publication: publication,
                unreadCount: appModel.sidebarTreeViewModel.unreadCount(for: publication)
            )
            .readerFullWidthTapLabel()
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(appModel.feedSelection == .publication(publication.publicationId) ? .isSelected : [])
        .accessibilityIdentifier("news-sidebar-publication-\(publication.publicationId)")
    }
}
