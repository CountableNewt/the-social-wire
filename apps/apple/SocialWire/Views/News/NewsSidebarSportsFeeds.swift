import SwiftUI

struct NewsSidebarSportsFeeds: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let isSelected: Bool
    let onSelect: (NewsTabSlot) -> Void

    var body: some View {
        let topic = appModel.sportsTopic
        let feeds = SportsNamedFeed.followedFeeds(
            topic.catalog?.feeds ?? [], entities: topic.catalog?.entities ?? [],
            selections: topic.selections
        )
        ForEach(feeds) { feed in
            NewsSidebarButton(title: SportsEntity.displayName(feed.title, kind: feed.kind),
                              systemImage: "sportscourt",
                              isSelected: isSelected && topic.selectedFeedID == feed.id,
                              identifier: "news-sidebar-sports-\(feed.id)") {
                onSelect(.sports)
                Task { await topic.selectFeed(feed.id, language: Locale.current.language.languageCode?.identifier ?? "en") }
            }
        }
    }
}
