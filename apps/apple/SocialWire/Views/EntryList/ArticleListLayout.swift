import SwiftUI

struct ArticleListLayout<Content: View>: View {
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    #endif

    let notice: String?
    private let spacing: CGFloat
    private let content: Content

    init(notice: String? = nil, spacing: CGFloat = 16, @ViewBuilder content: () -> Content) {
        self.notice = notice
        self.spacing = spacing
        self.content = content()
    }

    private var horizontalMargin: CGFloat {
        #if os(iOS)
        horizontalSizeClass == .regular ? 40 : 24
        #else
        40
        #endif
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: spacing) {
                if let notice {
                    Label(notice, systemImage: "exclamationmark.triangle")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(notice)
                }
                content
            }
            .padding(.vertical)
            .padding(.horizontal, horizontalMargin)
            .frame(maxWidth: ArticleReadingWidth.feed, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .center)
        }
    }
}
