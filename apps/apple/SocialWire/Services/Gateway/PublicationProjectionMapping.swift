import Foundation

enum PublicationProjectionMapping {
    static func publicationPrefsMap(
        from rows: [PublicationPrefsDTO]
    ) -> [String: RepoRecord<PublicationPrefsRecord>] {
        var map: [String: RepoRecord<PublicationPrefsRecord>] = [:]
        for row in rows {
            let raw = row.value ?? [:]
            let folderId = raw["folderId"]?.string
            let sortOrder = raw["sortOrder"].flatMap { value -> Int? in
                if case .number(let n) = value { return Int(n) }
                return nil
            }
            let hidden: Bool? = {
                guard let value = raw["hidden"] else { return nil }
                if case .bool(let flag) = value { return flag }
                return nil
            }()
            let createdAt = raw["createdAt"]?.string ?? row.publicationId

            map[row.publicationId] = RepoRecord(
                uri: row.uri,
                cid: "",
                value: PublicationPrefsRecord(
                    type: PDSRecordService.publicationPrefs,
                    publicationId: row.publicationId,
                    folderId: folderId,
                    sortOrder: sortOrder,
                    hidden: hidden,
                    createdAt: createdAt
                )
            )
        }
        return map
    }

    static func folders(from rows: [PublicationFolderDTO]?) -> [RepoRecord<FolderRecord>] {
        (rows ?? []).compactMap { row in
            let raw = row.value ?? [:]
            guard let name = raw["name"]?.string else { return nil }
            let sortOrder = raw["sortOrder"].flatMap { value -> Int? in
                if case .number(let n) = value { return Int(n) }
                return nil
            }
            let createdAt = raw["createdAt"]?.string ?? row.rkey
            return RepoRecord(
                uri: row.uri,
                cid: "",
                value: FolderRecord(
                    type: PDSRecordService.folder,
                    name: name,
                    sortOrder: sortOrder,
                    icon: raw["icon"]?.string,
                    iconImage: raw["iconImage"]?.string,
                    createdAt: createdAt
                )
            )
        }
        .sorted { ($0.value.sortOrder ?? 0, $0.value.name) < ($1.value.sortOrder ?? 0, $1.value.name) }
    }

    static func folderMap(
        from sections: [PublicationFolderSectionDTO]?
    ) -> [String: [DiscoveredPublication]]? {
        guard let sections, !sections.isEmpty else { return nil }
        var map: [String: [DiscoveredPublication]] = [:]
        for section in sections {
            map[section.folderRkey] = section.publications.map { $0.toDiscoveredPublication() }
        }
        return map
    }

    static func folderMap(
        allRows: [DiscoveredPublication],
        myPublications: [DiscoveredPublication],
        followingTab: [DiscoveredPublication],
        publicationPrefs: [String: RepoRecord<PublicationPrefsRecord>]
    ) -> [String: [DiscoveredPublication]] {
        let myIds = Set(myPublications.map(\.publicationId))
        let followingIds = Set(followingTab.map(\.publicationId))
        var folderMap: [String: [DiscoveredPublication]] = [:]

        for publication in allRows {
            guard !myIds.contains(publication.publicationId) else { continue }
            guard !followingIds.contains(publication.publicationId) else { continue }
            guard let folderId = publicationPrefs[publication.publicationId]?.value.folderId?
                .trimmingCharacters(in: .whitespacesAndNewlines),
                !folderId.isEmpty
            else { continue }
            folderMap[folderId, default: []].append(publication)
        }
        return folderMap
    }

    static func unreadCountsMap(from projection: PublicationSidebarResponseDTO) -> [String: Int] {
        var map: [String: Int] = [:]

        func applyCount(publicationId: String, count: Int?) {
            guard let count, count > 0 else { return }
            map[publicationId] = count
        }

        let recordMap = projection.unreadCountsByPublicationId
        for row in projection.sidebarPublicationRows() {
            let count: Int?
            if recordMap != nil {
                // Record map is authoritative once present — do not resurrect stale embedded row counts.
                let fromRecord = PublicationUnreadCountLookup.lookup(
                    in: recordMap ?? [:],
                    publicationId: row.publicationId
                )
                count = fromRecord > 0 ? fromRecord : nil
            } else {
                count = row.unreadCount
            }
            applyCount(publicationId: row.publicationId, count: count)
        }

        if let unreadCountsByPublicationId = recordMap {
            for (publicationId, count) in unreadCountsByPublicationId
                where PublicationUnreadCountLookup.lookup(in: map, publicationId: publicationId) == 0 {
                applyCount(publicationId: publicationId, count: count)
            }
        }

        return map
    }

    /// Mirrors web `applyUnreadCountsEvent` — updates record map and embedded row counts.
    static func applyingUnreadCounts(
        to projection: PublicationSidebarResponseDTO,
        counts: [String: Int],
        replacePublicationIds: [String]? = nil
    ) -> PublicationSidebarResponseDTO {
        var record = projection.unreadCountsByPublicationId ?? [:]

        if let replacePublicationIds {
            for publicationId in replacePublicationIds {
                let fresh = PublicationUnreadCountLookup.lookup(in: counts, publicationId: publicationId)
                if fresh > 0 {
                    record[normalizeATRepoParam(publicationId)] = fresh
                } else {
                    PublicationUnreadCountLookup.remove(for: publicationId, from: &record)
                }
            }
        } else {
            for (publicationId, count) in counts {
                PublicationUnreadCountLookup.store(count, for: publicationId, in: &record)
            }
        }

        func patchRow(_ row: SidebarPublicationRowDTO) -> SidebarPublicationRowDTO {
            if let replacePublicationIds,
               replacePublicationIds.contains(where: { PublicationUnreadCountLookup.publicationIdsMatch($0, row.publicationId) })
            {
                let count = PublicationUnreadCountLookup.lookup(in: counts, publicationId: row.publicationId)
                return row.withUnreadCount(count > 0 ? count : 0)
            }
            if let count = counts[row.publicationId] {
                return row.withUnreadCount(count)
            }
            let lookedUp = PublicationUnreadCountLookup.lookup(in: counts, publicationId: row.publicationId)
            if lookedUp > 0 {
                return row.withUnreadCount(lookedUp)
            }
            return row
        }

        return PublicationSidebarResponseDTO(
            viewerDid: projection.viewerDid,
            folders: projection.folders,
            publicationPrefs: projection.publicationPrefs,
            folderSections: projection.folderSections?.map { section in
                PublicationFolderSectionDTO(
                    folderRkey: section.folderRkey,
                    folderUri: section.folderUri,
                    publications: section.publications.map(patchRow)
                )
            },
            allPublicationRows: projection.allPublicationRows.map(patchRow),
            myPublications: projection.myPublications.map(patchRow),
            subscribedUnfoldered: projection.subscribedUnfoldered.map(patchRow),
            followingTabPublications: projection.followingTabPublications.map(patchRow),
            enrollAuthorDids: projection.enrollAuthorDids,
            refreshedAt: projection.refreshedAt,
            unreadCountsByPublicationId: record
        )
    }
}

extension SidebarPublicationRowDTO {
    func withUnreadCount(_ unreadCount: Int?) -> SidebarPublicationRowDTO {
        SidebarPublicationRowDTO(
            publicationId: publicationId,
            subscriptionPublicationId: subscriptionPublicationId,
            authorDid: authorDid,
            authorHandle: authorHandle,
            title: title,
            iconUrl: iconUrl,
            avatarUrl: avatarUrl,
            discoveredAt: discoveredAt,
            appViewScope: appViewScope,
            unreadCount: unreadCount
        )
    }

    func toDiscoveredPublication() -> DiscoveredPublication {
        DiscoveredPublication(
            publicationId: publicationId,
            subscriptionPublicationId: subscriptionPublicationId,
            authorDid: authorDid,
            authorHandle: authorHandle ?? authorDid,
            title: title,
            iconUrl: iconUrl,
            avatarUrl: avatarUrl,
            publicationSiteUrls: appViewScope.publicationSiteUrls,
            discoveredAt: discoveredAt
        )
    }
}

extension PublicationSidebarResponseDTO {
    /// All sidebar rows (deduped), mirroring web `sidebarPublicationRows`.
    func sidebarPublicationRows() -> [SidebarPublicationRowDTO] {
        var byId: [String: SidebarPublicationRowDTO] = [:]
        func add(_ row: SidebarPublicationRowDTO) {
            byId[normalizeATRepoParam(row.publicationId)] = row
        }
        for row in allPublicationRows { add(row) }
        for row in myPublications { add(row) }
        for row in subscribedUnfoldered { add(row) }
        for row in followingTabPublications { add(row) }
        for section in folderSections ?? [] {
            for row in section.publications { add(row) }
        }
        return Array(byId.values)
    }

    func scopesByPublicationId() -> [String: PublicationAppViewScopeDTO] {
        var scopes: [String: PublicationAppViewScopeDTO] = [:]
        for row in sidebarPublicationRows() {
            scopes[row.publicationId] = row.appViewScope
        }
        return scopes
    }
}
