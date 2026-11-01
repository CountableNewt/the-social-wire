import SwiftUI

struct NewsSidebarFolderRow: View {
    @Environment(SocialWireAppModel.self) private var appModel
    let folder: RepoRecord<FolderRecord>
    let onSelect: (NewsTabSlot) -> Void

    var body: some View {
        let key = rkey(from: folder.uri)
        DisclosureGroup(isExpanded: Binding(
            get: { appModel.sidebarExpandedFolderRkeys.contains(key) },
            set: { expanded in
                if expanded != appModel.sidebarExpandedFolderRkeys.contains(key) {
                    appModel.toggleSidebarFolderExpanded(rkey: key)
                }
            }
        )) {
            ForEach(appModel.publications(in: folder)) { publication in
                NewsSidebarPublicationRow(publication: publication, onSelect: onSelect)
            }
        } label: {
            Button {
                onSelect(.subscribed)
                Task { await appModel.selectFolderFeed(folderRkey: key) }
            } label: {
                HStack {
                    Label(folder.value.name, systemImage: "folder")
                    Spacer()
                    SidebarCountLabel(count: appModel.sidebarTreeViewModel.folderUnread(rkey: key))
                }
                .readerFullWidthTapLabel()
            }
            .buttonStyle(.plain)
            .accessibilityAddTraits(appModel.feedSelection == .folder(key) ? .isSelected : [])
            .accessibilityIdentifier("news-sidebar-folder-\(key)")
        }
    }
}
