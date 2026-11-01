import SwiftUI

struct NewsSectionPager<Content: View, EmptyContent: View>: View {
    let items: [NewsNavigationItem]
    @Binding var selection: NewsTabSlot
    var usesSidebarNavigation = false
    @ViewBuilder let content: (NewsTabSlot) -> Content
    @ViewBuilder let emptyContent: () -> EmptyContent

    var body: some View {
        if items.isEmpty {
            emptyContent()
        } else if usesSidebarNavigation {
            content(selection)
        } else {
            Group {
                #if os(iOS)
                TabView(selection: $selection) {
                    ForEach(items) { item in
                        // Reader feeds share a selected source; only mount the active
                        // destination so adjacent pages cannot load or mutate that source.
                        Group {
                            if item.slot == selection {
                                content(item.slot)
                            } else {
                                ProgressView()
                            }
                        }
                        .tag(item.slot)
                    }
                }
                .tabViewStyle(.page(indexDisplayMode: .never))
                #else
                content(selection)
                #endif
            }
            .safeAreaInset(edge: .top, spacing: 0) {
                NewsFeedPills(items: items, selection: $selection)
            }
        }
    }
}
