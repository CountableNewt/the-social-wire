import SwiftUI

struct SavedLinkRow: View {
    let save: MergedLatrSave

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(save.title)
                .font(.headline)
                .lineLimit(2)
            if let host = save.url?.host {
                Text(host)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(save.savedAt)
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
        .padding(.vertical, 4)
    }
}
