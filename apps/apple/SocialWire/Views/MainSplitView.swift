import SwiftUI

struct MainSplitView: View {
    @EnvironmentObject var authService: ATProtoOAuthService
    @StateObject private var viewModel = MainViewModel()
    @State private var selectedSection: AppSection? = .read

    var body: some View {
        NavigationSplitView {
            List(selection: $selectedSection) {
                Section {
                    Label("Reading List", systemImage: "newspaper")
                        .tag(AppSection.read)
                    Label("Saved Links", systemImage: "archivebox")
                        .tag(AppSection.saved)
                    Label("My Publications", systemImage: "person.crop.square")
                        .tag(AppSection.myPublications)
                }

                if !viewModel.unfolderedPublications.isEmpty {
                    Section("Publications") {
                        ForEach(viewModel.unfolderedPublications) { pub in
                            PublicationRowView(publication: pub)
                                .tag(AppSection.publication(pub.publicationId))
                        }
                    }
                }

                ForEach(viewModel.folders) { folder in
                    Section(folder.name) {
                        ForEach(viewModel.publications(in: folder)) { pub in
                            PublicationRowView(publication: pub)
                                .tag(AppSection.publication(pub.publicationId))
                        }
                    }
                }

                if !viewModel.followingPublications.isEmpty || !viewModel.hiddenPublications.isEmpty {
                    Section("More") {
                        if !viewModel.followingPublications.isEmpty {
                            Label("Following", systemImage: "person.2")
                                .tag(AppSection.following)
                        }
                        if !viewModel.hiddenPublications.isEmpty {
                            Label("Hidden", systemImage: "eye.slash")
                                .tag(AppSection.hidden)
                        }
                    }
                }

                Section {
                    Label("Settings", systemImage: "gearshape")
                        .tag(AppSection.settings)
                }
            }
            .listStyle(.sidebar)
            .navigationTitle("The Social Wire")
            .toolbar {
                ToolbarItem(placement: .navigationBarTrailing) {
                    Button {
                        Task { await viewModel.refresh() }
                    } label: {
                        Label("Refresh", systemImage: "arrow.clockwise")
                    }
                    .disabled(viewModel.isRefreshing)
                }
            }
        } content: {
            contentColumn
        } detail: {
            detailColumn
        }
        .task {
            guard let session = authService.session else { return }
            await viewModel.load(session: session)
        }
        .onChange(of: selectedSection) { _, next in
            viewModel.selectSection(next)
        }
        .alert("Something went wrong", isPresented: $viewModel.showsError) {
            Button("OK", role: .cancel) { viewModel.errorMessage = nil }
        } message: {
            Text(viewModel.errorMessage ?? "Unknown error")
        }
    }

    @ViewBuilder
    private var contentColumn: some View {
        switch selectedSection ?? .read {
        case .read:
            ReadingOverviewView(viewModel: viewModel)
        case .publication(let id):
            if let pub = viewModel.publication(id: id) {
                EntryListView(publication: pub, viewModel: viewModel)
                    .navigationTitle(pub.title)
            } else {
                ContentUnavailableView("Publication Missing", systemImage: "newspaper")
            }
        case .saved:
            SavedLinksView(viewModel: viewModel)
        case .myPublications:
            PublicationCollectionView(
                title: "My Publications",
                publications: viewModel.myPublications,
                emptyTitle: "No Publications",
                emptyMessage: "Publications attributed to your DID will appear here."
            ) { pub in
                selectedSection = .publication(pub.publicationId)
            }
        case .following:
            PublicationCollectionView(
                title: "Following",
                publications: viewModel.followingPublications,
                emptyTitle: "No Followed Publications",
                emptyMessage: "Followed accounts with Standard Site entries appear here."
            ) { pub in
                selectedSection = .publication(pub.publicationId)
            }
        case .hidden:
            PublicationCollectionView(
                title: "Hidden",
                publications: viewModel.hiddenPublications,
                emptyTitle: "No Hidden Publications",
                emptyMessage: "Hidden publications will appear here."
            ) { pub in
                selectedSection = .publication(pub.publicationId)
            }
        case .settings:
            SettingsView(viewModel: viewModel)
        }
    }

    @ViewBuilder
    private var detailColumn: some View {
        if let saved = viewModel.selectedSavedLink {
            SavedLinkDetailView(link: saved, viewModel: viewModel)
        } else if let entry = viewModel.selectedEntry {
            EntryDetailView(entry: entry, onSave: { detail in
                await viewModel.saveReadLater(from: detail)
            })
        } else {
            ContentUnavailableView("Select an Item", systemImage: "doc.text", description: Text("Choose an article or saved link to preview."))
        }
    }
}

enum AppSection: Hashable {
    case read
    case saved
    case myPublications
    case publication(String)
    case following
    case hidden
    case settings
}

@MainActor
final class MainViewModel: ObservableObject {
    @Published var folders: [FolderModel] = []
    @Published var publications: [PublicationModel] = []
    @Published var publicationPrefs: [String: RepoRecord<PublicationPrefsRecord>] = [:]
    @Published var entries: [EntryModel] = []
    @Published var savedLinks: [SavedLinkModel] = []
    @Published var selectedPublication: PublicationModel?
    @Published var selectedEntry: EntryModel?
    @Published var selectedSavedLink: SavedLinkModel?
    @Published var articleFilter: ArticleFilter = .all
    @Published var readAtByEntryId: [String: Date] = [:]
    @Published var isRefreshing = false
    @Published var isLoadingEntries = false
    @Published var isLoadingSavedLinks = false
    @Published var errorMessage: String?

    private var session: AuthSession?
    private var pdsClient: PDSClient?

    var showsError: Bool {
        get { errorMessage != nil }
        set { if !newValue { errorMessage = nil } }
    }

    var unfolderedPublications: [PublicationModel] {
        publications.filter { !$0.isOwnedByViewer && $0.folderId == nil && !(publicationPrefs[$0.publicationId]?.value.hidden ?? false) }
    }

    var myPublications: [PublicationModel] {
        publications.filter(\.isOwnedByViewer)
    }

    var followingPublications: [PublicationModel] {
        publications.filter { !$0.isOwnedByViewer && !(publicationPrefs[$0.publicationId]?.value.hidden ?? false) }
    }

    var hiddenPublications: [PublicationModel] {
        publications.filter { publicationPrefs[$0.publicationId]?.value.hidden ?? false }
    }

    var filteredEntries: [EntryModel] {
        switch articleFilter {
        case .all:
            return entries
        case .unread:
            return entries.filter { readAtByEntryId[$0.entryId] == nil }
        }
    }

    func load(session: AuthSession) async {
        self.session = session
        self.pdsClient = PDSClient(session: session)
        await refresh()
    }

    func refresh() async {
        guard let session, let pdsClient else { return }
        isRefreshing = true
        defer { isRefreshing = false }

        async let foldersTask = loadFolders()
        async let prefsTask = loadPublicationPrefs()
        async let discoveryTask = pdsClient.discoveredPublications(for: session.did)
        async let readStateTask = pdsClient.listEntryReadStates()
        async let savedTask = pdsClient.listMergedLatrSaves()

        do {
            folders = await foldersTask
            publicationPrefs = await prefsTask
            readAtByEntryId = try await readStateTask
            savedLinks = try await savedTask
            publications = mergeSubscriptions(into: try await discoveryTask)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func selectSection(_ section: AppSection?) {
        selectedSavedLink = nil
        if case .publication(let id) = section, let pub = publication(id: id) {
            selectPublication(pub)
        }
    }

    func publication(id: String) -> PublicationModel? {
        publications.first { $0.publicationId == id }
    }

    func publications(in folder: FolderModel) -> [PublicationModel] {
        publications.filter { $0.folderId == folder.id && !(publicationPrefs[$0.publicationId]?.value.hidden ?? false) }
    }

    func selectPublication(_ pub: PublicationModel) {
        selectedPublication = pub
        selectedEntry = nil
        selectedSavedLink = nil
        Task { await loadEntries(for: pub) }
    }

    func selectEntry(_ entry: EntryModel) {
        selectedEntry = entry
        selectedSavedLink = nil
        if articleFilter == .all {
            markEntryRead(entry)
        }
    }

    func markEntryRead(_ entry: EntryModel) {
        let now = Date()
        readAtByEntryId[entry.entryId] = now
        Task { try? await pdsClient?.putEntryReadState(subjectURI: entry.entryId, readAt: now) }
    }

    func markEntryUnread(_ entry: EntryModel) {
        readAtByEntryId.removeValue(forKey: entry.entryId)
        Task { try? await pdsClient?.deleteEntryReadState(subjectURI: entry.entryId) }
    }

    func selectSavedLink(_ link: SavedLinkModel) {
        selectedSavedLink = link
        selectedEntry = nil
    }

    func reloadSavedLinks() async {
        guard let pdsClient else { return }
        isLoadingSavedLinks = true
        defer { isLoadingSavedLinks = false }
        do {
            savedLinks = try await pdsClient.listMergedLatrSaves()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func saveReadLater(from detail: EntryDetailModel) async {
        guard let url = detail.originalURL else { return }
        do {
            try await pdsClient?.saveReadLater(url: url, title: detail.title, excerpt: detail.summary)
            await reloadSavedLinks()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func archiveSavedLink(_ link: SavedLinkModel) {
        Task {
            do {
                try await pdsClient?.archiveSavedLink(link)
                if selectedSavedLink?.id == link.id { selectedSavedLink = nil }
                await reloadSavedLinks()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func deleteSavedLink(_ link: SavedLinkModel) {
        Task {
            await pdsClient?.deleteSavedLink(link)
            if selectedSavedLink?.id == link.id { selectedSavedLink = nil }
            await reloadSavedLinks()
        }
    }

    func addPublication(_ value: String) {
        Task {
            do {
                if value.hasPrefix("http") {
                    try await pdsClient?.createSkyreaderFeedSubscription(feedURL: value, title: nil)
                } else {
                    try await pdsClient?.createPublicationSubscription(publication: value)
                }
                await refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    func hidePublication(_ pub: PublicationModel, hidden: Bool) {
        Task {
            do {
                try await pdsClient?.upsertPublicationPrefs(
                    publicationId: pub.publicationId,
                    hidden: hidden,
                    existing: publicationPrefs[pub.publicationId]
                )
                await refresh()
            } catch {
                errorMessage = error.localizedDescription
            }
        }
    }

    private func loadFolders() async -> [FolderModel] {
        do { return try await pdsClient?.listFolders() ?? [] } catch { return [] }
    }

    private func loadPublicationPrefs() async -> [String: RepoRecord<PublicationPrefsRecord>] {
        do {
            let prefs = try await pdsClient?.listPublicationPrefs() ?? []
            return Dictionary(uniqueKeysWithValues: prefs.map { ($0.value.publicationId, $0) })
        } catch {
            return [:]
        }
    }

    private func mergeSubscriptions(into discovered: [PublicationModel]) -> [PublicationModel] {
        var rows = discovered
        let prefs = publicationPrefs
        for index in rows.indices {
            rows[index].folderId = prefs[rows[index].publicationId]?.value.folderId
        }
        return rows.sorted { $0.title.localizedCaseInsensitiveCompare($1.title) == .orderedAscending }
    }

    private func loadEntries(for pub: PublicationModel) async {
        guard let pdsClient else { return }
        isLoadingEntries = true
        defer { isLoadingEntries = false }
        do {
            entries = try await pdsClient.entries(for: pub.publicationId)
        } catch {
            errorMessage = error.localizedDescription
        }
    }
}

enum ArticleFilter: String, CaseIterable, Identifiable {
    case all = "All"
    case unread = "Unread"

    var id: String { rawValue }
}
