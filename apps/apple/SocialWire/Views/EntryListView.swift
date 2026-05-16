import SwiftUI

/// Column 2: Scrollable list of entries for the selected publication.
struct EntryListView: View {
    let publication: PublicationModel
    @ObservedObject var viewModel: MainViewModel

    var body: some View {
        VStack(spacing: 0) {
            Picker("Article Filter", selection: $viewModel.articleFilter) {
                ForEach(ArticleFilter.allCases) { filter in
                    Text(filter.rawValue).tag(filter)
                }
            }
            .pickerStyle(.segmented)
            .padding(12)

            List(selection: Binding(
                get: { viewModel.selectedEntry?.entryId },
                set: { id in
                    if let id, let entry = viewModel.filteredEntries.first(where: { $0.entryId == id }) {
                        viewModel.selectEntry(entry)
                    }
                }
            )) {
                ForEach(viewModel.filteredEntries) { entry in
                    EntryRowView(
                        entry: entry,
                        isRead: viewModel.readAtByEntryId[entry.entryId] != nil,
                        markRead: { viewModel.markEntryRead(entry) },
                        markUnread: { viewModel.markEntryUnread(entry) }
                    )
                    .tag(entry.entryId)
                }
            }
            .listStyle(.plain)
            .overlay {
                if viewModel.isLoadingEntries {
                    ProgressView()
                } else if viewModel.filteredEntries.isEmpty {
                    ContentUnavailableView(
                        viewModel.articleFilter == .unread ? "No Unread Articles" : "No Entries",
                        systemImage: "doc.text",
                        description: Text("This publication has no matching entries.")
                    )
                }
            }
        }
    }
}

struct EntryRowView: View {
    let entry: EntryModel
    var isRead = false
    var markRead: () -> Void = {}
    var markUnread: () -> Void = {}

    private var formattedDate: String {
        let date = entry.publishedAt
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter.string(from: date)
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle()
                .fill(isRead ? Color.clear : Color.accentColor)
                .stroke(.tertiary, lineWidth: isRead ? 1 : 0)
                .frame(width: 8, height: 8)
                .padding(.top, 6)

            VStack(alignment: .leading, spacing: 4) {
                Text(entry.title)
                    .font(.subheadline.weight(isRead ? .regular : .semibold))
                    .lineLimit(2)

                if let summary = entry.summary {
                    Text(summary)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }

                Text(formattedDate)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            if isRead {
                Button("Unread", systemImage: "envelope.badge") {
                    markUnread()
                }
                .tint(.blue)
            } else {
                Button("Read", systemImage: "checkmark") {
                    markRead()
                }
                .tint(.green)
            }
        }
        .contextMenu {
            if isRead {
                Button("Mark Unread", systemImage: "envelope.badge", action: markUnread)
            } else {
                Button("Mark Read", systemImage: "checkmark", action: markRead)
            }
            if let url = entry.originalURL {
                Link(destination: url) {
                    Label("Open Original", systemImage: "safari")
                }
            }
        }
    }
}

struct ReadingOverviewView: View {
    @ObservedObject var viewModel: MainViewModel

    var body: some View {
        List {
            Section("My Publications") {
                ForEach(viewModel.myPublications) { pub in
                    PublicationSummaryButton(publication: pub) {
                        viewModel.selectPublication(pub)
                    }
                }
            }

            Section("Reading List") {
                ForEach(viewModel.unfolderedPublications) { pub in
                    PublicationSummaryButton(publication: pub) {
                        viewModel.selectPublication(pub)
                    }
                }
            }
        }
        .overlay {
            if viewModel.publications.isEmpty {
                ContentUnavailableView(
                    "No Publications",
                    systemImage: "newspaper",
                    description: Text("Follow or add publications, then refresh.")
                )
            }
        }
        .navigationTitle("Reading List")
    }
}

private struct PublicationSummaryButton: View {
    let publication: PublicationModel
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                AsyncImage(url: publication.iconURL ?? publication.avatarURL) { image in
                    image.resizable().scaledToFill()
                } placeholder: {
                    Image(systemName: publication.source == .rss ? "dot.radiowaves.left.and.right" : "newspaper")
                        .foregroundStyle(.secondary)
                }
                .frame(width: 36, height: 36)
                .clipShape(RoundedRectangle(cornerRadius: 8))

                VStack(alignment: .leading, spacing: 2) {
                    Text(publication.title)
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    Text(publication.publicationId)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
    }
}

struct PublicationCollectionView: View {
    let title: String
    let publications: [PublicationModel]
    let emptyTitle: String
    let emptyMessage: String
    let onSelect: (PublicationModel) -> Void

    var body: some View {
        List(publications) { pub in
            PublicationSummaryButton(publication: pub) {
                onSelect(pub)
            }
        }
        .overlay {
            if publications.isEmpty {
                ContentUnavailableView(emptyTitle, systemImage: "newspaper", description: Text(emptyMessage))
            }
        }
        .navigationTitle(title)
    }
}

struct SavedLinksView: View {
    @ObservedObject var viewModel: MainViewModel

    var body: some View {
        List(selection: Binding(
            get: { viewModel.selectedSavedLink?.id },
            set: { id in
                if let id, let link = viewModel.savedLinks.first(where: { $0.id == id }) {
                    viewModel.selectSavedLink(link)
                }
            }
        )) {
            ForEach(viewModel.savedLinks) { link in
                SavedLinkRow(link: link)
                    .tag(link.id)
                    .swipeActions {
                        Button("Archive", systemImage: "archivebox") {
                            viewModel.archiveSavedLink(link)
                        }
                        .tint(.orange)
                        Button("Delete", systemImage: "trash", role: .destructive) {
                            viewModel.deleteSavedLink(link)
                        }
                    }
            }
        }
        .overlay {
            if viewModel.isLoadingSavedLinks {
                ProgressView()
            } else if viewModel.savedLinks.isEmpty {
                ContentUnavailableView(
                    "Nothing Saved",
                    systemImage: "archivebox",
                    description: Text("Use Save on an article toolbar to add its HTTPS URL.")
                )
            }
        }
        .navigationTitle("Saved Links")
        .refreshable {
            await viewModel.reloadSavedLinks()
        }
    }
}

private struct SavedLinkRow: View {
    let link: SavedLinkModel

    var body: some View {
        HStack(spacing: 12) {
            AsyncImage(url: link.imageURL) { image in
                image.resizable().scaledToFill()
            } placeholder: {
                Image(systemName: "link")
                    .foregroundStyle(.secondary)
            }
            .frame(width: 48, height: 48)
            .clipShape(RoundedRectangle(cornerRadius: 8))

            VStack(alignment: .leading, spacing: 4) {
                Text(link.title)
                    .font(.subheadline.weight(.medium))
                    .lineLimit(2)
                if let excerpt = link.excerpt {
                    Text(excerpt)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(2)
                }
                Text("\(link.subtitle) · \(link.savedAt.formatted(date: .abbreviated, time: .shortened))")
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 4)
    }
}

struct SavedLinkDetailView: View {
    let link: SavedLinkModel
    @ObservedObject var viewModel: MainViewModel

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(link.title)
                        .font(.headline)
                        .lineLimit(1)
                    Text(link.subtitle)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer()
                if let url = link.url {
                    Link(destination: url) {
                        Label("Open", systemImage: "safari")
                    }
                    .buttonStyle(.bordered)
                }
                Button("Archive", systemImage: "archivebox") {
                    viewModel.archiveSavedLink(link)
                }
                .buttonStyle(.bordered)
                Button("Delete", systemImage: "trash", role: .destructive) {
                    viewModel.deleteSavedLink(link)
                }
                .buttonStyle(.bordered)
            }
            .padding()
            .background(.bar)

            if let url = link.url {
                WebPreview(url: url)
            } else {
                ContentUnavailableView("Preview Unavailable", systemImage: "doc.text", description: Text(link.subjectURI))
            }
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject var authService: ATProtoOAuthService
    @ObservedObject var viewModel: MainViewModel
    @State private var publicationInput = ""

    var body: some View {
        Form {
            Section("Add Publication") {
                TextField("DID, publication AT-URI, or RSS URL", text: $publicationInput)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                Button("Add Publication", systemImage: "plus") {
                    let value = publicationInput.trimmingCharacters(in: .whitespacesAndNewlines)
                    guard !value.isEmpty else { return }
                    viewModel.addPublication(value)
                    publicationInput = ""
                }
            }

            Section("Reader") {
                Picker("Article Filter", selection: $viewModel.articleFilter) {
                    ForEach(ArticleFilter.allCases) { filter in
                        Text(filter.rawValue).tag(filter)
                    }
                }
            }

            Section("Account") {
                Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) {
                    Task { await authService.signOut() }
                }
            }
        }
        .navigationTitle("Settings")
    }
}
