import SwiftUI

struct NewsFeedPills: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let items: [NewsNavigationItem]
    @Binding var selection: NewsTabSlot

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(items) { item in
                        Button {
                            withAnimation(reduceMotion ? nil : .snappy) {
                                selection = item.slot
                            }
                        } label: {
                            Text(item.title)
                                .font(.subheadline)
                                .fontWeight(selection == item.slot ? .semibold : .regular)
                                .fixedSize()
                                .padding(.horizontal, 14)
                                .padding(.vertical, 8)
                                .background(
                                    selection == item.slot ? Color.accentColor.opacity(0.18) : Color.secondary.opacity(0.1),
                                    in: Capsule()
                                )
                                .frame(minHeight: 44)
                                .contentShape(Capsule())
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(selection == item.slot ? .isSelected : [])
                        .accessibilityIdentifier("news-feed-pill-\(item.title)")
                        .id(item.slot)
                    }
                }
                .padding(.horizontal)
            }
            .scrollIndicators(.hidden)
            .onChange(of: selection, initial: true) { _, slot in
                withAnimation(reduceMotion ? nil : .snappy) {
                    proxy.scrollTo(slot, anchor: .center)
                }
            }
        }
        .accessibilityIdentifier("news-feed-pills")
    }
}
