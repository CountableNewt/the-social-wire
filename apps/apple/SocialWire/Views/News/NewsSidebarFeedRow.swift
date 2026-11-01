import SwiftUI

struct NewsSidebarFeedRow: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let item: NewsNavigationItem
    let isSelected: Bool
    let onSelect: (NewsTabSlot) -> Void

    private var hasChildren: Bool {
        [.readLater, .subscribed, .finance, .sports].contains(item.slot)
    }

    var body: some View {
        @Bindable var appModel = appModel
        if hasChildren {
            DisclosureGroup(isExpanded: $appModel[sidebarFeedExpanded: item.slot]) {
                NewsSidebarFeedChildren(slot: item.slot, isSelected: isSelected, onSelect: onSelect)
            } label: {
                NewsSidebarButton(title: item.title, systemImage: item.systemImage,
                                  isSelected: isSelected && isAggregateSelected,
                                  identifier: "news-sidebar-feed-\(item.title)") {
                    onSelect(item.slot)
                    if item.slot == .finance {
                        Task { await appModel.selectFinanceFeed("finance") }
                    } else if item.slot == .sports {
                        Task { await appModel.sportsTopic.selectFeed("sports", language: Locale.current.language.languageCode?.identifier ?? "en") }
                    }
                }
            }
        } else {
            NewsSidebarButton(title: item.title, systemImage: item.systemImage,
                              isSelected: isSelected, identifier: "news-sidebar-feed-\(item.title)") {
                onSelect(item.slot)
            }
        }
    }

    private var isAggregateSelected: Bool {
        switch item.slot {
        case .subscribed: appModel.feedSelection == .topLevel(.subscribed)
        case .readLater: appModel.selectedSavedSourceKey == nil
        case .finance: appModel.selectedFinanceFeedID == "finance"
        case .sports: appModel.sportsTopic.selectedFeedID == "sports"
        default: true
        }
    }
}
