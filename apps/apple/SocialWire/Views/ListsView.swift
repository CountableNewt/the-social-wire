import SwiftUI

struct ListsView: View {
    @Environment(SocialWireAppModel.self) private var appModel
    private let onListSourceTap: (ReaderListSource) -> Void
    @State private var refreshFeedback = 0

    init(onListSourceTap: @escaping (ReaderListSource) -> Void) {
        self.onListSourceTap = onListSourceTap
    }

    var body: some View {
        List {
            ForEach(ReaderListSource.allCases) { source in
                Button {
                    onListSourceTap(source)
                } label: {
                    HStack {
                        Label(source.rawValue, systemImage: source.systemImage)
                        Spacer(minLength: 8)
                        if appModel.readerListSource == source {
                            Image(systemName: "checkmark")
                                .foregroundStyle(Color.accentColor)
                                .accessibilityHidden(true)
                        }
                    }
                    .readerFullWidthTapLabel()
                }
                .buttonStyle(.plain)
                .readerClearListRow()
                .accessibilityAddTraits(appModel.readerListSource == source ? .isSelected : [])
            }
        }
        .readerListCanvas()
        .refreshable {
            await appModel.refreshAll()
            refreshFeedback += 1
        }
        .sensoryFeedback(.impact(flexibility: .soft), trigger: refreshFeedback)
    }
}
