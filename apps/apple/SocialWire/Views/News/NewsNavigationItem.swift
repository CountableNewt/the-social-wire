import Foundation

struct NewsNavigationItem: Identifiable, Hashable {
    let slot: NewsTabSlot
    let title: String
    let systemImage: String

    var id: NewsTabSlot { slot }

    init(slot: NewsTabSlot, title: String, systemImage: String) {
        self.slot = slot
        self.title = title
        self.systemImage = systemImage
    }

    init(feed: NewsPrimaryFeed) {
        slot = NewsTabSlot(feed)
        title = feed.title
        systemImage = feed.systemImage
    }
}
