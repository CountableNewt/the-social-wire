import SwiftUI

struct EntryListView: View {
    @Environment(SocialWireAppModel.self) private var appModel

    var body: some View {
        List {
            if appModel.isLoadingEntries {
                ProgressView()
                    .frame(maxWidth: .infinity)
            } else if appModel.filteredEntries.isEmpty {
                ContentUnavailableView("No Articles", systemImage: "doc.text")
            } else {
                Section("Articles") {
                    ForEach(appModel.filteredEntries) { entry in
                        EntryRow(entry: entry, isRead: appModel.readAtByEntryId[entry.entryId] != nil)
                            .contentShape(Rectangle())
                            .onTapGesture {
                                Task { await appModel.selectEntry(entry) }
                            }
                            .swipeActions(edge: .trailing) {
                                Button(appModel.readAtByEntryId[entry.entryId] == nil ? "Read" : "Unread") {
                                    Task { await appModel.toggleRead(entry) }
                                }
                                .tint(.indigo)
                            }
                    }
                }
            }
        }
        .refreshable {
            if let publication = appModel.selectedPublication {
                await appModel.loadEntries(for: publication)
            }
        }
    }
}
