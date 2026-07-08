import Foundation
import GatewayCore
import Hummingbird
import Logging
import NIOCore
import ThinAppViewCore

struct BootstrapStreamService {
  let projectionService: PublicationProjectionService
  let readService: ThinAppViewReadService
  let enrollService: ThinAppViewEnrollService
  let skyreaderIngestionService: ThinAppViewSkyreaderIngestionService
  let projectionCache: (any AppViewProjectionCacheStore)?
  let logger: Logger

  private enum SidebarBootstrapChunk: Sendable {
    case unreadCounts([String: Int])
    case folders(AppViewBootstrapSidebarFoldersPayload)
  }

  func writeStream(auth: AuthContext, writer: inout any ResponseBodyWriter) async throws {
    let streamStarted = Date()
    let refreshedAt = Date()
    do {
      if let cached = try await loadFreshCachedSnapshot(viewerDid: auth.did) {
        try await emitCachedBootstrap(
          auth: auth,
          snapshot: cached,
          refreshedAt: refreshedAt,
          writer: &writer
        )
        scheduleBackgroundWarmers(
          auth: auth,
          priority: cached.priority,
          enrollAuthorDids: cached.priority.enrollAuthorDids
        )
        scheduleBackgroundRefresh(auth: auth, priority: cached.priority)
        BootstrapStreamTimings.logPhase(
          logger,
          phase: "totalCached",
          startedAt: streamStarted,
          viewerDid: auth.did
        )
        try await writer.finish(nil)
        return
      }

      let priorityStarted = Date()
      let priority = try await projectionService.bootstrapPrioritySidebar(auth: auth)
      try await writeEvent(.sidebarPriority(priority.response), writer: &writer)
      BootstrapStreamTimings.logPhase(
        logger,
        phase: "prioritySidebar",
        startedAt: priorityStarted,
        viewerDid: auth.did
      )

      let unreadRows = priority.response.allPublicationRows
        + priority.response.myPublications
        + priority.response.subscribedUnfoldered
        + priority.response.followingTabPublications

      let unreadStarted = Date()
      let foldersStarted = Date()
      var unreadCounts: [String: Int] = [:]
      var folders: AppViewBootstrapSidebarFoldersPayload?
      var selectedId: String?
      var selectedRow: SidebarPublicationRow?
      var selectedEnrollTask: Task<Void, Never>?
      var emittedSelectedEntries = false

      try await withThrowingTaskGroup(of: SidebarBootstrapChunk.self) { group in
        group.addTask {
          .unreadCounts(
            await projectionService.freshUnreadCountsMap(
              for: unreadRows,
              viewerDid: auth.did
            )
          )
        }
        group.addTask {
          .folders(
            try await projectionService.bootstrapFolderSidebar(
              auth: auth,
              context: priority.context
            )
          )
        }

        for try await chunk in group {
          switch chunk {
          case .unreadCounts(let counts):
            unreadCounts = counts
            try await writeEvent(.unreadCounts(counts), writer: &writer)
            BootstrapStreamTimings.logPhase(
              logger,
              phase: "unreadCounts",
              startedAt: unreadStarted,
              viewerDid: auth.did,
              extra: ["publicationCount": "\(counts.count)"]
            )

            selectedId = BootstrapStreamSelection.firstUnreadPublicationId(
              myPublications: priority.response.myPublications,
              subscribedUnfoldered: priority.response.subscribedUnfoldered,
              following: priority.response.followingTabPublications,
              unreadCounts: counts
            )
            selectedRow = selectedId.flatMap {
              BootstrapStreamSelection.row(publicationId: $0, in: priority.response)
            }
            selectedEnrollTask = selectedRow.map { row in
              Task { await self.enrollAuthorForBootstrap(auth: auth, row: row) }
            }
            if let selectedId, let selectedRow {
              emittedSelectedEntries = true
              try await writeEvent(.selectedPublication(publicationId: selectedId), writer: &writer)
              let entriesStarted = Date()
              try await writeBootstrapEntriesPage(
                auth: auth,
                publicationId: selectedId,
                row: selectedRow,
                enrollTask: selectedEnrollTask,
                writer: &writer
              )
              BootstrapStreamTimings.logPhase(
                logger,
                phase: "entriesPage",
                startedAt: entriesStarted,
                viewerDid: auth.did
              )
            }

          case .folders(let payload):
            folders = payload
            try await writeSidebarSections(
              payload,
              unreadCounts: nil,
              refreshedAt: refreshedAt,
              writer: &writer
            )
            try await writeEvent(.sidebarFolders(payload), writer: &writer)
            BootstrapStreamTimings.logPhase(
              logger,
              phase: "folderSidebar",
              startedAt: foldersStarted,
              viewerDid: auth.did
            )
          }
        }
      }

      guard let folders else {
        throw HTTPError(.internalServerError, message: "Bootstrap folder sidebar did not complete.")
      }

      let priorityPublicationIds = Set(unreadRows.map(\.publicationId))
      let folderUnreadRows = folders.allPublicationRows.filter {
        !priorityPublicationIds.contains($0.publicationId)
      }
      var folderUnreadCounts: [String: Int] = [:]
      if !folderUnreadRows.isEmpty {
        let folderUnreadStarted = Date()
        folderUnreadCounts = await projectionService.freshUnreadCountsMap(
          for: folderUnreadRows,
          viewerDid: auth.did
        )
        let folderPublicationIds = folderUnreadRows.map(\.publicationId)
        try await writeSidebarSections(
          folders,
          unreadCounts: folderUnreadCounts,
          refreshedAt: refreshedAt,
          writer: &writer
        )
        try await writeEvent(
          .unreadCounts(folderUnreadCounts, replacePublicationIds: folderPublicationIds),
          writer: &writer
        )
        BootstrapStreamTimings.logPhase(
          logger,
          phase: "folderUnreadCounts",
          startedAt: folderUnreadStarted,
          viewerDid: auth.did,
          extra: ["publicationCount": "\(folderUnreadCounts.count)"]
        )
      }

      if !emittedSelectedEntries, let selectedId, let selectedRow {
        try await writeEvent(.selectedPublication(publicationId: selectedId), writer: &writer)
        let entriesStarted = Date()
        try await writeBootstrapEntriesPage(
          auth: auth,
          publicationId: selectedId,
          row: selectedRow,
          enrollTask: selectedEnrollTask,
          writer: &writer
        )
        BootstrapStreamTimings.logPhase(
          logger,
          phase: "selectedEntryPage",
          startedAt: entriesStarted,
          viewerDid: auth.did,
          extra: ["publicationId": selectedId]
        )

        scheduleSelectedPublicationWarmers(
          auth: auth,
          row: selectedRow,
          priorityAuthorDids: BootstrapStreamSelection.priorityAuthorDids(from: priority.response),
          skipAuthorEnroll: selectedEnrollTask != nil
        )
      }

      try await writeEvent(.done(refreshedAt: refreshedAt), writer: &writer)

      let cacheExpires = Date().addingTimeInterval(AppViewProjectionCacheTTL.sidebarSeconds)
      let unreadExpires = Date().addingTimeInterval(AppViewProjectionCacheTTL.unreadCountsSeconds)
      if let projectionCache {
        let snapshot = BootstrapSidebarCacheSnapshot(
          priority: priority.response,
          folderPayload: folders
        )
        if let data = try? JSONEncoder().encode(snapshot),
           let json = String(data: data, encoding: .utf8)
        {
          try? await projectionCache.storeSidebarProjectionJSON(
            viewerDid: auth.did,
            jsonBody: json,
            expiresAt: cacheExpires
          )
        }
        try? await projectionCache.storeUnreadCounts(
          viewerDid: auth.did,
          counts: unreadCounts.merging(folderUnreadCounts) { current, _ in current },
          expiresAt: unreadExpires
        )
      }

      scheduleBackgroundWarmers(
        auth: auth,
        priority: priority.response,
        enrollAuthorDids: priority.response.enrollAuthorDids
      )

      BootstrapStreamTimings.logPhase(
        logger,
        phase: "total",
        startedAt: streamStarted,
        viewerDid: auth.did
      )
    } catch {
      logger.error(
        "Bootstrap stream failed",
        metadata: ["error": .string(String(describing: error))]
      )
      try await writeEvent(.error(error.localizedDescription), writer: &writer)
      try await writeEvent(.done(refreshedAt: refreshedAt), writer: &writer)
    }
    try await writer.finish(nil)
  }

  private func loadFreshCachedSnapshot(viewerDid: String) async throws -> BootstrapSidebarCacheSnapshot? {
    guard let projectionCache else { return nil }
    guard let json = try await projectionCache.cachedSidebarProjectionJSON(viewerDid: viewerDid) else {
      return nil
    }
    return try? JSONDecoder().decode(BootstrapSidebarCacheSnapshot.self, from: Data(json.utf8))
  }

  private func emitCachedBootstrap(
    auth: AuthContext,
    snapshot: BootstrapSidebarCacheSnapshot,
    refreshedAt: Date,
    writer: inout any ResponseBodyWriter
  ) async throws {
    try await writeEvent(.sidebarPriority(snapshot.priority), writer: &writer)

    let unreadRows = Self.cachedBootstrapUnreadRows(from: snapshot)
    let unreadPublicationIds = unreadRows.map(\.publicationId)
    let cachedUnreadCounts = await cachedBootstrapUnreadCounts(viewerDid: auth.did)
    let unreadCounts: [String: Int]
    let freshUnreadCountsTask: Task<[String: Int], Never>?
    if let cachedUnreadCounts {
      unreadCounts = cachedUnreadCounts
      freshUnreadCountsTask = Task {
        await self.projectionService.freshUnreadCountsMap(for: unreadRows, viewerDid: auth.did)
      }
      try await writeEvent(.unreadCounts(unreadCounts), writer: &writer)
    } else {
      unreadCounts = await projectionService.freshUnreadCountsMap(for: unreadRows, viewerDid: auth.did)
      freshUnreadCountsTask = nil
      await storeCachedUnreadCounts(unreadCounts, viewerDid: auth.did)
      try await writeEvent(
        .unreadCounts(unreadCounts, replacePublicationIds: unreadPublicationIds),
        writer: &writer
      )
    }

    if let folders = snapshot.folderPayload {
      try await writeSidebarSections(
        folders,
        unreadCounts: cachedUnreadCounts,
        refreshedAt: refreshedAt,
        writer: &writer
      )
      try await writeEvent(.sidebarFolders(folders), writer: &writer)
    }

    if let selectedId = BootstrapStreamSelection.firstUnreadPublicationId(
      myPublications: snapshot.priority.myPublications,
      subscribedUnfoldered: snapshot.priority.subscribedUnfoldered,
      following: snapshot.priority.followingTabPublications,
      unreadCounts: unreadCounts
    ),
      let row = BootstrapStreamSelection.row(publicationId: selectedId, in: snapshot.priority)
    {
      let selectedEnrollTask = Task { await self.enrollAuthorForBootstrap(auth: auth, row: row) }
      try await writeEvent(.selectedPublication(publicationId: selectedId), writer: &writer)
      try await writeBootstrapEntriesPage(
        auth: auth,
        publicationId: selectedId,
        row: row,
        enrollTask: selectedEnrollTask,
        writer: &writer
      )
    }

    if let freshUnreadCountsTask {
      let freshUnreadCounts = await freshUnreadCountsTask.value
      await storeCachedUnreadCounts(freshUnreadCounts, viewerDid: auth.did)
      if let folders = snapshot.folderPayload {
        try await writeSidebarSections(
          folders,
          unreadCounts: freshUnreadCounts,
          refreshedAt: refreshedAt,
          writer: &writer
        )
      }
      try await writeEvent(
        .unreadCounts(freshUnreadCounts, replacePublicationIds: unreadPublicationIds),
        writer: &writer
      )
    }

    try await writeEvent(.done(refreshedAt: refreshedAt), writer: &writer)
  }

  private func scheduleBackgroundRefresh(auth: AuthContext, priority: PublicationSidebarResponse) {
    Task {
      do {
        _ = try await self.projectionService.refreshFullDiscoverySidebar(auth: auth)
        let refreshed = try await self.projectionService.bootstrapPrioritySidebar(auth: auth)
        let folders = try await self.projectionService.bootstrapFolderSidebar(
          auth: auth,
          context: refreshed.context
        )
        guard let projectionCache else { return }
        let cacheExpires = Date().addingTimeInterval(AppViewProjectionCacheTTL.sidebarSeconds)
        let snapshot = BootstrapSidebarCacheSnapshot(
          priority: refreshed.response,
          folderPayload: folders
        )
        if let data = try? JSONEncoder().encode(snapshot),
           let json = String(data: data, encoding: .utf8)
        {
          try? await projectionCache.storeSidebarProjectionJSON(
            viewerDid: auth.did,
            jsonBody: json,
            expiresAt: cacheExpires
          )
        }
        _ = priority
      } catch {
        self.logger.warning(
          "Background sidebar refresh failed",
          metadata: ["error": .string(String(describing: error))]
        )
      }
    }
  }

  private func scheduleBackgroundWarmers(
    auth: AuthContext,
    priority: PublicationSidebarResponse,
    enrollAuthorDids: [String]
  ) {
    let priorityAuthorDids = BootstrapStreamSelection.priorityAuthorDids(from: priority)
    Task {
      if !priorityAuthorDids.isEmpty {
        do {
          _ = try await self.enrollService.enroll(
            auth: auth,
            authorDids: priorityAuthorDids,
            recentOnly: true
          )
        } catch {
          self.logger.warning(
            "Bootstrap stream priority enroll failed",
            metadata: ["error": .string(String(describing: error))]
          )
        }
      }

      do {
        _ = try await self.skyreaderIngestionService.ingestViewerSubscriptions(auth: auth)
      } catch {
        self.logger.warning(
          "Bootstrap stream Skyreader RSS ingest failed",
          metadata: ["error": .string(String(describing: error))]
        )
      }

      let warmedAuthorDids = Set(priorityAuthorDids)
      let remainingAuthorDids = enrollAuthorDids.filter {
        ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid($0) && !warmedAuthorDids.contains($0)
      }
      if !remainingAuthorDids.isEmpty {
        do {
          _ = try await self.enrollService.enroll(
            auth: auth,
            authorDids: remainingAuthorDids,
            recentOnly: false
          )
        } catch {
          self.logger.warning(
            "Bootstrap stream bulk enroll failed",
            metadata: ["error": .string(String(describing: error))]
          )
        }
      }
    }
  }

  private func scheduleSelectedPublicationWarmers(
    auth: AuthContext,
    row: SidebarPublicationRow,
    priorityAuthorDids: [String],
    skipAuthorEnroll: Bool = false
  ) {
    Task {
      await self.warmSelectedPublication(
        auth: auth,
        row: row,
        priorityAuthorDids: priorityAuthorDids,
        skipAuthorEnroll: skipAuthorEnroll
      )
    }
  }

  private func warmSelectedPublication(
    auth: AuthContext,
    row: SidebarPublicationRow,
    priorityAuthorDids: [String],
    skipAuthorEnroll: Bool = false
  ) async {
    let warmedAuthorDids = Set(priorityAuthorDids)
    if row.publicationId.hasPrefix(PublicationLexicons.rssPublicationPrefix),
       let feedUrl = PublicationProjectionLogic.normalizedFeedUrlFromRssPublicationId(row.publicationId)
    {
      do {
        _ = try await skyreaderIngestionService.ingestViewerSubscriptions(
          auth: auth,
          priorityFeedUrls: [feedUrl]
        )
      } catch {
        logger.warning(
          "Bootstrap stream selected RSS feed ingest failed",
          metadata: ["publicationId": .string(row.publicationId)]
        )
      }
    } else if !skipAuthorEnroll, !warmedAuthorDids.contains(row.appViewScope.authorDid) {
      do {
        _ = try await enrollService.enroll(
          auth: auth,
          authorDids: [row.appViewScope.authorDid],
          recentOnly: true
        )
      } catch {
        logger.warning(
          "Bootstrap stream selected publication enroll failed",
          metadata: ["publicationId": .string(row.publicationId)]
        )
      }
    }
  }

  private func writeBootstrapEntriesPage(
    auth: AuthContext,
    publicationId: String,
    row: SidebarPublicationRow,
    enrollTask: Task<Void, Never>?,
    writer: inout any ResponseBodyWriter
  ) async throws {
    // If PDS backfill finished while folder sidebar loaded, prefer a fresh index read.
    if await enrollAlreadyFinished(enrollTask),
       let page = try await readService.liveFirstPage(auth: auth, scope: row.appViewScope, limit: 50)
    {
      try await emitBootstrapEntriesPage(publicationId: publicationId, page: page, writer: &writer)
      return
    }

    // Stale-first: paint cached page 1 without blocking on PDS backfill.
    if let page = try await readService.cachedFirstPageIfAvailable(
      auth: auth,
      publicationId: publicationId,
      scope: row.appViewScope,
      limit: 50
    ) {
      try await emitBootstrapEntriesPage(publicationId: publicationId, page: page, writer: &writer)
      return
    }

    // Cold path: do not block the stream on PDS enroll — client refreshes feed after `done`.
    if let enrollTask {
      Task { await enrollTask.value }
    } else {
      Task { await enrollAuthorForBootstrap(auth: auth, row: row) }
    }
    try await emitBootstrapEntriesPage(
      publicationId: publicationId,
      page: AppViewEntryListResponse(entries: [], cursor: nil),
      writer: &writer
    )
  }

  /// Stale-first unread map for cached bootstrap; fresh replacement is emitted by `emitCachedBootstrap`.
  private func cachedBootstrapUnreadCounts(viewerDid: String) async -> [String: Int]? {
    if let projectionCache,
       let cached = try? await projectionCache.cachedUnreadCounts(viewerDid: viewerDid),
       !cached.isEmpty
    {
      return cached
    }
    return nil
  }

  private func storeCachedUnreadCounts(_ counts: [String: Int], viewerDid: String) async {
    guard let projectionCache, !counts.isEmpty else { return }
    let expiresAt = Date().addingTimeInterval(AppViewProjectionCacheTTL.unreadCountsSeconds)
    try? await projectionCache.storeUnreadCounts(
      viewerDid: viewerDid,
      counts: counts,
      expiresAt: expiresAt
    )
  }

  private static func cachedBootstrapUnreadRows(
    from snapshot: BootstrapSidebarCacheSnapshot
  ) -> [SidebarPublicationRow] {
    var rows: [SidebarPublicationRow] = []
    var seen = Set<String>()
    let append = { (row: SidebarPublicationRow) in
      guard !seen.contains(row.publicationId) else { return }
      seen.insert(row.publicationId)
      rows.append(row)
    }
    for row in snapshot.priority.allPublicationRows { append(row) }
    for row in snapshot.priority.myPublications { append(row) }
    for row in snapshot.priority.subscribedUnfoldered { append(row) }
    for row in snapshot.priority.followingTabPublications { append(row) }
    for row in snapshot.folderPayload?.allPublicationRows ?? [] { append(row) }
    return rows
  }

  private func emitBootstrapEntriesPage(
    publicationId: String,
    page: AppViewEntryListResponse,
    writer: inout any ResponseBodyWriter
  ) async throws {
    let payload = AppViewBootstrapEntriesPagePayload(
      publicationId: publicationId,
      entries: page.entries.map(Self.bootstrapEntry),
      cursor: page.cursor
    )
    try await writeEvent(.entriesPage(payload), writer: &writer)
  }

  private func writeSidebarSections(
    _ payload: AppViewBootstrapSidebarFoldersPayload,
    unreadCounts: [String: Int]?,
    refreshedAt: Date,
    writer: inout any ResponseBodyWriter
  ) async throws {
    for section in payload.folderSections {
      let publicationIds = section.publications.map(\.publicationId)
      let sectionCounts: [String: Int]?
      if let unreadCounts {
        sectionCounts = Dictionary(
          uniqueKeysWithValues: publicationIds.map { ($0, unreadCounts[$0] ?? 0) }
        )
      } else {
        sectionCounts = nil
      }
      try await writeEvent(
        .sidebarSection(
          AppViewBootstrapSidebarSectionPayload(
            sectionKey: "folder:\(section.folderRkey)",
            folderRkey: section.folderRkey,
            folderUri: section.folderUri,
            publications: section.publications,
            unreadCounts: sectionCounts,
            replacePublicationIds: unreadCounts == nil ? nil : publicationIds,
            refreshedAt: refreshedAt
          )
        ),
        writer: &writer
      )
    }
  }

  /// Returns true when enroll finished without waiting (used to pick live vs cached bootstrap page 1).
  private func enrollAlreadyFinished(_ task: Task<Void, Never>?) async -> Bool {
    guard let task else { return false }
    return await withTaskGroup(of: Bool.self) { group in
      group.addTask {
        await task.value
        return true
      }
      group.addTask {
        await Task.yield()
        return false
      }
      let finished = await group.next() ?? false
      group.cancelAll()
      return finished
    }
  }

  /// PDS backfill for bootstrap page 1 — runs in parallel with folder sidebar when possible.
  private func enrollAuthorForBootstrap(auth: AuthContext, row: SidebarPublicationRow) async {
    if row.publicationId.hasPrefix(PublicationLexicons.rssPublicationPrefix),
       let feedUrl = PublicationProjectionLogic.normalizedFeedUrlFromRssPublicationId(row.publicationId)
    {
      _ = try? await skyreaderIngestionService.ingestViewerSubscriptions(
        auth: auth,
        priorityFeedUrls: [feedUrl]
      )
      return
    }

    let authorDid = row.appViewScope.authorDid
    guard ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(authorDid) else { return }
    do {
      _ = try await enrollService.enroll(
        auth: auth,
        authorDids: [authorDid],
        recentOnly: true
      )
    } catch {
      logger.warning(
        "Bootstrap stream pre-page enroll failed",
        metadata: [
          "publicationId": .string(row.publicationId),
          "error": .string(String(describing: error)),
        ]
      )
    }
  }

  private static func bootstrapEntry(_ item: AppViewEntryListItem) -> AppViewBootstrapEntryListItem {
    AppViewBootstrapEntryListItem(
      entryId: item.entryId,
      title: item.title,
      summary: item.summary,
      publishedAt: item.publishedAt,
      thumbnailUrl: item.thumbnailUrl,
      thumbnailFallbackUrl: item.thumbnailFallbackUrl,
      originalUrl: item.originalUrl
    )
  }

  private func writeEvent(
    _ event: AppViewBootstrapStreamEvent,
    writer: inout any ResponseBodyWriter
  ) async throws {
    let line = try AppViewBootstrapStreamNDJSON.encodeLine(event)
    var buffer = ByteBuffer()
    buffer.writeBytes(line)
    try await writer.write(buffer)
  }
}
