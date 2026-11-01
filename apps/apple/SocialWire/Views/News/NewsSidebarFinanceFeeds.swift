import SwiftUI

struct NewsSidebarFinanceFeeds: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let isSelected: Bool
    let onSelect: (NewsTabSlot) -> Void

    var body: some View {
        let feeds = FinanceNamedFeed.followedFeeds(
            appModel.financeFeeds, selections: appModel.financeSelections,
            hideCrypto: appModel.feedPreferences.hideFinanceCrypto
        )
        ForEach(feeds) { feed in
            NewsSidebarButton(title: feed.title, systemImage: feed.systemImage,
                              isSelected: isSelected && appModel.selectedFinanceFeedID == feed.id,
                              identifier: "news-sidebar-finance-\(feed.id)") {
                onSelect(.finance)
                Task { await appModel.selectFinanceFeed(feed.id) }
            }
        }
    }
}
