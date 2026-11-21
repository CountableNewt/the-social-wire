import SwiftUI

/// Footer status for an explicit feed refresh, like Mail's update line: progress while
/// publications are checked, then the result briefly.
struct FeedRefreshStatusBar: View {
    @Environment(SocialWireAppModel.self) private var appModel

    var body: some View {
        Group {
            if let progress = appModel.feedRefreshProgress {
                VStack(spacing: 6) {
                    Text(progress.label)
                        .contentTransition(.numericText())
                    if let fraction = progress.fraction {
                        ProgressView(value: fraction)
                            .frame(maxWidth: 160)
                    } else {
                        ProgressView()
                            .controlSize(.small)
                    }
                }
                .accessibilityElement(children: .combine)
            } else if let status = appModel.feedRefreshStatus {
                Text(status)
                    .task(id: status) {
                        try? await Task.sleep(for: .seconds(3))
                        if appModel.feedRefreshStatus == status { appModel.feedRefreshStatus = nil }
                    }
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .padding(.vertical, 8)
        .frame(maxWidth: .infinity)
        .animation(.default, value: appModel.feedRefreshProgress)
        .animation(.default, value: appModel.feedRefreshStatus)
        .accessibilityAddTraits(.updatesFrequently)
    }
}
