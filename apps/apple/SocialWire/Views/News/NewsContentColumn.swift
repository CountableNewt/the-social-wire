import SwiftUI

struct NewsContentColumn: View {
    let selectedTab: NewsTab
    let sceneModel: NewsSceneModel

    var body: some View {
        Group {
            switch selectedTab {
            case .library:
                LibraryNewsView(sceneModel: sceneModel)
            case .saved:
                SavedNewsView(sceneModel: sceneModel)
            case .search:
                PublicationSearchView(query: "")
            }
        }
    }
}
