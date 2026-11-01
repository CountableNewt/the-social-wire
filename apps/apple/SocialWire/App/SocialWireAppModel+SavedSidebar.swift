import Foundation

extension SocialWireAppModel {
    var sembleFeedSources: [SavedFeedSource] {
        let groups = Dictionary(grouping: sembleItems.compactMap { item -> SavedFeedSource? in
            guard let host = item.sourceHost else { return nil }
            return SavedFeedSource(
                id: "site:\(host)",
                model: SavedLinkPublicationChipModel(
                    name: item.siteName?.isEmpty == false ? item.siteName ?? host : host,
                    faviconURL: nil, homepageURL: URL(string: "https://\(host)")
                ),
                count: 1
            )
        }, by: \.id)
        return groups.compactMap { key, sources in
            guard let first = sources.first else { return nil }
            return SavedFeedSource(id: key, model: first.model, count: sources.count)
        }
        .sorted { $0.model.name.localizedCaseInsensitiveCompare($1.model.name) == .orderedAscending }
    }

    var filteredSembleItems: [SembleCollectionItem] {
        guard let selectedSavedSourceKey else { return sembleItems }
        return sembleItems.filter { item in
            item.sourceHost.map { "site:\($0)" } == selectedSavedSourceKey
        }
    }
}
