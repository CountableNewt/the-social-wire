import SwiftUI

struct NewsSidebarFeedChildren: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let slot: NewsTabSlot
    let isSelected: Bool
    let onSelect: (NewsTabSlot) -> Void

    var body: some View {
        switch slot {
        case .subscribed:
            ForEach(appModel.folders) { folder in
                NewsSidebarFolderRow(folder: folder, onSelect: onSelect)
            }
            ForEach(appModel.subscribedUnfolderedPublications) { publication in
                NewsSidebarPublicationRow(publication: publication, onSelect: onSelect)
            }
        case .readLater:
            ForEach(appModel.savedFeedSources(for: .readLater)) { source in
                NewsSidebarButton(title: source.model.name, systemImage: "newspaper",
                                  isSelected: isSelected && appModel.selectedSavedSourceKey == source.id,
                                  identifier: "news-sidebar-saved-source-\(source.id)") {
                    onSelect(.readLater)
                    appModel.selectSavedFeedSource(source)
                }
            }
        case .finance:
            NewsSidebarFinanceFeeds(isSelected: isSelected, onSelect: onSelect)
        case .sports:
            NewsSidebarSportsFeeds(isSelected: isSelected, onSelect: onSelect)
        default:
            EmptyView()
        }
    }
}
