import Foundation
import Logging

public struct ScheduleWorker: Sendable {
    public let store: SQLiteStore
    public let pdsClient: any PDSClient
    public let logger: Logger
    private let linkPreviewHydrator: any LinkPreviewHydrating
    private let requiresAuthoritativeCalendar: Bool
    private let allowSchedulesWithoutCalendar: Bool
    private let maxAttempts = 8

    public init(
        store: SQLiteStore,
        pdsClient: any PDSClient,
        logger: Logger,
        linkPreviewHydrator: (any LinkPreviewHydrating)? = nil,
        requiresAuthoritativeCalendar: Bool = true,
        allowSchedulesWithoutCalendar: Bool = false
    ) {
        self.store = store
        self.pdsClient = pdsClient
        self.logger = logger
        self.linkPreviewHydrator = linkPreviewHydrator ?? LinkPreviewService(
            pdsClient: pdsClient,
            logger: logger
        )
        self.requiresAuthoritativeCalendar = requiresAuthoritativeCalendar
        self.allowSchedulesWithoutCalendar = allowSchedulesWithoutCalendar
    }

    public func runTick(now: Date = Date()) async {
        let nowString = Timestamp.iso8601(now)
        do {
            try await unblockReadyDependencies(nowString: nowString)
            for _ in 0..<10 {
                guard !Task.isCancelled, let job = try await store.claimDueJobs(now: nowString, limit: 1).first else { break }
                await publish(job: job, now: now, nowString: nowString)
            }
        } catch {
            logger.error("schedule worker tick failed: \(String(describing: error))")
        }
    }

    private func unblockReadyDependencies(nowString: String) async throws {
        let blocked = try await store.blockedJobs()
        for job in blocked {
            guard let dependency = job.dependsOnScheduleUri,
                  let parent = try await store.findPublishedSchedule(scheduleUri: dependency),
                  let parentPublishedUri = parent.record.publishedUri
            else { continue }
            let leaseID = UUID().uuidString
            let leaseNow = Timestamp.date(from: nowString) ?? Date()
            guard try await store.claimScheduleEdit(did: job.did, rkey: job.rkey, leaseID: leaseID,
                now: nowString, expiresAt: Timestamp.iso8601(leaseNow.addingTimeInterval(300))) else { continue }
            do {
                if var record = try await pdsClient.getScheduleForMutation(did: job.did, rkey: job.rkey) {
                    let relationship = record.dependency?.relationship ?? .after
                    let parentPublishedCid = parent.record.publishedCid ?? parent.record.publishedPosts.first?.cid
                    if relationship != .after, parentPublishedCid == nil {
                        try await store.releaseScheduleEdit(did: job.did, rkey: job.rkey, leaseID: leaseID)
                        continue
                    }
                    record.status = .scheduled
                    record.lastError = nil
                    record.dependency = ScheduleDependency(
                        dependsOnScheduleUri: dependency,
                        relationship: relationship,
                        parentPublishedUri: parentPublishedUri,
                        parentPublishedCid: parentPublishedCid
                    )
                    record.updatedAt = nowString
                    try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
                }
                try await store.markJobScheduled(
                    did: job.did,
                    rkey: job.rkey,
                    parentPublishedUri: parentPublishedUri,
                    now: nowString
                )
                try await store.insertAuditEvent(
                    did: job.did,
                    scheduleRkey: job.rkey,
                    action: "dependency_unblocked",
                    message: "Parent published as \(parentPublishedUri).",
                    now: nowString
                )
                try await store.releaseScheduleEdit(did: job.did, rkey: job.rkey, leaseID: leaseID)
            } catch {
                try? await store.releaseScheduleEdit(did: job.did, rkey: job.rkey, leaseID: leaseID)
                throw error
            }
        }
    }

    private func publish(job: ScheduledJob, now: Date, nowString: String) async {
        do {
            guard var record = try await pdsClient.getScheduleForMutation(did: job.did, rkey: job.rkey) else {
                throw ScheduleError(code: .recordInvalid, message: "Schedule record missing from PDS")
            }

            guard record.status == .scheduled, record.publishedUri == nil,
                  record.publishedCid == nil, record.publishedPosts.isEmpty else {
                var projection = job
                projection.status = record.status == .scheduled ? .publishing : record.status
                projection.publishedUri = record.publishedUri
                projection.publishedCid = record.publishedCid
                try await store.upsertScheduleJob(projection, now: nowString)
                return
            }

            if requiresAuthoritativeCalendar && (!allowSchedulesWithoutCalendar || record.calendarEventUri != nil) {
                guard let reconciled = await reconcileAuthoritativeCalendar(
                    job: job,
                    record: record,
                    now: now,
                    nowString: nowString
                ) else { return }
                record = reconciled
            }

            if record.recordType == "app.bsky.feed.post" {
                let previousRkey = record.publishRkey
                let changed = assignStablePublishRkeys(record: &record, now: now)
                if changed {
                    record.updatedAt = nowString
                    try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
                    try await store.updatePublishRkey(
                        did: job.did,
                        rkey: job.rkey,
                        publishRkey: record.publishRkey,
                        now: nowString
                    )
                    try await store.insertAuditEvent(
                        did: job.did,
                        scheduleRkey: job.rkey,
                        action: "publish_rkey_upgraded",
                        message: "Assigned stable AT Protocol publish rkeys (first: \(previousRkey) → \(record.publishRkey)).",
                        now: nowString
                    )
                }
            }

            guard let scheduledDate = Timestamp.date(from: record.scheduledAt) else {
                throw ScheduleError(code: .recordInvalid, message: "Invalid scheduled publish time")
            }
            if scheduledDate > now {
                var updated = job
                updated.scheduledAt = Timestamp.iso8601(scheduledDate)
                try await store.upsertScheduleJob(updated, now: nowString)
                return
            }
            guard ["app.bsky.feed.post", "site.standard.document"].contains(record.recordType) else {
                throw ScheduleError(code: .recordInvalid, message: "Unsupported scheduled collection")
            }

            if let dependency = record.dependency {
                let parent = try await store.findPublishedSchedule(scheduleUri: dependency.dependsOnScheduleUri)
                let parentPublishedUri = parent?.record.publishedUri ?? parent?.record.publishedPosts.first?.uri
                let parentPublishedCid = parent?.record.publishedCid ?? parent?.record.publishedPosts.first?.cid
                guard let parentPublishedUri,
                      dependency.relationship == .after || parentPublishedCid != nil
                else {
                    let error = ScheduleError(
                        code: .parentUnavailable,
                        message: "Parent schedule has not published yet.",
                        classification: .parentMissing
                    )
                    record.status = .blocked
                    record.lastError = error
                    record.updatedAt = nowString
                    try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
                    try await store.markJobBlocked(did: job.did, rkey: job.rkey, error: error, now: nowString)
                    try await store.insertAuditEvent(
                        did: job.did,
                        scheduleRkey: job.rkey,
                        action: "dependency_blocked",
                        message: error.message,
                        now: nowString
                    )
                    return
                }
                record.dependency = ScheduleDependency(
                    dependsOnScheduleUri: dependency.dependsOnScheduleUri,
                    relationship: dependency.relationship,
                    parentPublishedUri: parentPublishedUri,
                    parentPublishedCid: parentPublishedCid
                )
                try await store.updateParentPublishedUri(
                    did: job.did,
                    rkey: job.rkey,
                    parentPublishedUri: parentPublishedUri,
                    now: nowString
                )
            }

            let hydration = await hydrateMissingExternalThumbnails(record: record, did: job.did)
            record = hydration.record
            if hydration.count > 0 {
                try await store.insertAuditEvent(
                    did: job.did,
                    scheduleRkey: job.rkey,
                    action: "link_preview_thumbnails_hydrated",
                    message: "Uploaded \(hydration.count) Open Graph thumbnail(s) before publishing.",
                    now: nowString
                )
            }

            record.status = .publishing
            record.retry.attemptCount = job.attempts + 1
            record.retry.lastAttemptAt = nowString
            record.retry.nextAttemptAt = nil
            record.lastError = nil
            record.updatedAt = nowString
            try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
            try await store.insertAuditEvent(
                did: job.did,
                scheduleRkey: job.rkey,
                action: "publish_attempt_started",
                message: "Publish attempt \(record.retry.attemptCount) started.",
                now: nowString
            )

            let publishedPosts = try await pdsClient.publishThread(did: job.did, record: record)
            guard let firstPublished = publishedPosts.first else {
                throw ScheduleError(code: .recordInvalid, message: "PDS returned no published posts")
            }
            let published = PublishedPost(uri: firstPublished.uri, cid: firstPublished.cid)
            record.status = .published
            record.publishedUri = published.uri
            record.publishedCid = published.cid
            record.publishedPosts = publishedPosts
            record.updatedAt = nowString
            record.lastError = nil
            try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
            try await store.markJobPublished(did: job.did, rkey: job.rkey, published: published, now: nowString)
            try await store.insertAuditEvent(
                did: job.did,
                scheduleRkey: job.rkey,
                action: "publish_succeeded",
                message: "Published \(published.uri).",
                now: nowString
            )
            logger.info("published schedule \(job.rkey) for \(job.did) as \(published.uri)")
        } catch let error as ScheduleError {
            await handlePublishError(error, job: job, now: now, nowString: nowString)
        } catch {
            await handlePublishError(Self.classify(error), job: job, now: now, nowString: nowString)
        }
    }

    private func reconcileAuthoritativeCalendar(
        job: ScheduledJob,
        record: SkejScheduleRecord,
        now: Date,
        nowString: String
    ) async -> SkejScheduleRecord? {
        do {
            let event = try await CalendarCoordinator(pdsClient: pdsClient)
                .requirePublishableEvent(did: job.did, rkey: job.rkey)
            guard event.startsAt != record.scheduledAt else { return record }

            var reconciled = record
            reconciled.scheduledAt = event.startsAt
            reconciled.updatedAt = nowString
            try await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: reconciled)

            try await store.insertAuditEvent(
                did: job.did,
                scheduleRkey: job.rkey,
                action: "calendar_reconciled",
                message: "Authoritative calendar timing reconciled to \(event.startsAt).",
                now: nowString
            )
            if let scheduledDate = Timestamp.date(from: event.startsAt), scheduledDate > now {
                var projectedJob = job
                projectedJob.scheduledAt = Timestamp.iso8601(scheduledDate)
                projectedJob.status = .scheduled
                projectedJob.lastError = nil
                projectedJob.nextAttemptAt = nil
                try await store.upsertScheduleJob(projectedJob, now: nowString)
                logger.info("deferred schedule \(job.rkey) to authoritative calendar time \(event.startsAt)")
                return nil
            }
            return reconciled
        } catch {
            let message: String
            switch error {
            case CalendarReconciliationError.eventMissing:
                message = "Authoritative calendar event is missing."
            case CalendarReconciliationError.eventInvalid:
                message = "Authoritative calendar event is invalid."
            case let CalendarReconciliationError.eventNotPublishable(status):
                message = "Authoritative calendar event is not publishable (\(status.rawValue))."
            default:
                message = "Authoritative calendar event could not be verified."
            }
            let scheduleError = ScheduleError(
                code: .recordInvalid,
                message: message,
                classification: .recordInvalid
            )
            var blockedRecord = record
            blockedRecord.status = .blocked
            blockedRecord.lastError = scheduleError
            blockedRecord.updatedAt = nowString
            try? await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: blockedRecord)
            try? await store.markJobBlocked(did: job.did, rkey: job.rkey, error: scheduleError, now: nowString)
            try? await store.insertAuditEvent(
                did: job.did,
                scheduleRkey: job.rkey,
                action: "calendar_blocked",
                message: message,
                now: nowString
            )
            logger.error("blocked schedule \(job.rkey): \(message)")
            return nil
        }
    }

    @discardableResult
    private func assignStablePublishRkeys(
        record: inout SkejScheduleRecord,
        now: Date
    ) -> Bool {
        var changed = false
        var assigned = Set<String>()
        record.posts = record.posts.enumerated().map { index, post in
            let candidate: String?
            if let existing = post.publishRkey,
               ATProtoTID.isValid(existing),
               !assigned.contains(existing) {
                candidate = existing
            } else if index == 0,
                      ATProtoTID.isValid(record.publishRkey),
                      !assigned.contains(record.publishRkey) {
                candidate = record.publishRkey
            } else {
                candidate = nil
            }
            let rkey = candidate ?? ATProtoTID.generate(date: now)
            assigned.insert(rkey)
            if post.publishRkey != rkey { changed = true }
            return PostPlan(
                text: post.text,
                source: post.source,
                publishRkey: rkey,
                facets: post.facets,
                reply: post.reply,
                embed: post.embed,
                langs: post.langs,
                labels: post.labels,
                tags: post.tags,
                unknownFields: post.unknownFields
            )
        }
        if let first = record.posts.first?.publishRkey,
           record.publishRkey != first {
            record.publishRkey = first
            changed = true
        }
        return changed
    }

    private func handlePublishError(_ error: ScheduleError, job: ScheduledJob, now: Date, nowString: String) async {
        let shouldRetry = Self.isRetryable(error) && job.attempts < maxAttempts
        if var record = try? await pdsClient.getScheduleForMutation(did: job.did, rkey: job.rkey) {
            record.status = shouldRetry ? .scheduled : .failed
            record.lastError = error
            record.retry.attemptCount = job.attempts + 1
            record.retry.lastAttemptAt = nowString
            record.retry.nextAttemptAt = shouldRetry ? nextAttemptDate(now: now, attempts: job.attempts + 1) : nil
            record.updatedAt = nowString
            try? await pdsClient.writeSchedule(did: job.did, rkey: job.rkey, record: record)
        }

        do {
            if error.classification == .authInvalid {
                try await store.markAccountNeedsReauth(did: job.did, now: nowString)
                try await store.insertAuditEvent(
                    did: job.did,
                    scheduleRkey: job.rkey,
                    action: "account_needs_reauth",
                    message: error.message,
                    now: nowString
                )
            }

            if shouldRetry {
                let nextAttemptAt = nextAttemptDate(now: now, attempts: job.attempts + 1)
                try await store.markJobRetry(
                    did: job.did,
                    rkey: job.rkey,
                    error: error,
                    nextAttemptAt: nextAttemptAt,
                    now: nowString
                )
                try await store.insertAuditEvent(
                    did: job.did,
                    scheduleRkey: job.rkey,
                    action: "retry_scheduled",
                    message: "Retry scheduled for \(nextAttemptAt): \(error.message)",
                    now: nowString
                )
            } else {
                try await store.markJobFailed(did: job.did, rkey: job.rkey, error: error, now: nowString)
                try await store.insertAuditEvent(
                    did: job.did,
                    scheduleRkey: job.rkey,
                    action: "publish_failed",
                    message: error.message,
                    now: nowString
                )
            }
        } catch {
            logger.error("failed to persist publish error for \(job.rkey): \(String(describing: error))")
        }
    }

    static func classify(_ error: Error) -> ScheduleError {
        if PDSClientError.isAuthFailure(error) {
            return ScheduleError(code: .authInvalid, message: "Reconnect this account before publishing.")
        }
        if case HTTPClientError.badStatus(let status, let body, let headers) = error {
            if status == 400 {
                if let oauthError = try? JSONDecoder().decode(OAuthErrorResponse.self, from: Data(body.utf8)) {
                    if oauthError.error == "invalid_client_metadata" {
                        return ScheduleError(
                            code: .transientNetwork,
                            message: "OAuth client metadata is temporarily unavailable."
                        )
                    }
                }
                return ScheduleError(code: .recordInvalid, message: "PDS rejected the record.")
            }
            if status == 404 { return ScheduleError(code: .recordInvalid, message: "The scheduled record is unavailable.") }
            if status == 429 {
                return ScheduleError(
                    code: .rateLimited,
                    message: "PDS rate limited this publish attempt.",
                    retryAfter: headers["retry-after"]
                )
            }
            if status >= 500 {
                return ScheduleError(code: .transientNetwork, message: "PDS is temporarily unavailable.")
            }
        }
        return ScheduleError(code: .unknown, message: "The publish request could not be completed.")
    }

    static func isRetryable(_ error: ScheduleError) -> Bool {
        switch error.classification {
        case .transientNetwork, .rateLimited, .unknown:
            true
        case .authInvalid, .recordInvalid, .parentMissing, .parentUnavailable:
            false
        }
    }

    private func nextAttemptDate(now: Date, attempts: Int) -> String {
        let capped = min(max(attempts, 1), 8)
        let base = min(pow(2.0, Double(capped)) * 60.0, 60.0 * 60.0)
        let jitter = Double.random(in: 0...30)
        return Timestamp.iso8601(now.addingTimeInterval(base + jitter))
    }

    private func hydrateMissingExternalThumbnails(
        record: SkejScheduleRecord,
        did: String
    ) async -> (record: SkejScheduleRecord, count: Int) {
        var updated = record
        var count = 0

        for index in updated.posts.indices {
            guard let url = missingExternalThumbnailURL(in: updated.posts[index].embed) else {
                continue
            }
            do {
                let embed = try await linkPreviewHydrator.hydrate(did: did, url: url)
                guard embed.external.thumb != nil else { continue }
                let post = updated.posts[index]
                updated.posts[index] = PostPlan(
                    text: post.text,
                    source: post.source,
                    publishRkey: post.publishRkey,
                    facets: post.facets,
                    reply: post.reply,
                    embed: try embed.skejJSONValue(),
                    langs: post.langs,
                    labels: post.labels,
                    tags: post.tags,
                    unknownFields: post.unknownFields
                )
                count += 1
            } catch {
                logger.warning(
                    "link preview thumbnail still unavailable at publish time",
                    metadata: [
                        "host": "\(URL(string: url)?.host ?? "unknown")",
                        "category": "\(String(describing: type(of: error)))",
                    ]
                )
            }
        }

        if let shadowRecord = updated.shadowRecord,
           case var .object(post) = shadowRecord,
           let url = missingExternalThumbnailURL(in: post["embed"])
        {
            do {
                let embed = try await linkPreviewHydrator.hydrate(did: did, url: url)
                if embed.external.thumb != nil {
                    post["embed"] = try embed.skejJSONValue()
                    updated.shadowRecord = .object(post)
                    count += 1
                }
            } catch {
                logger.warning(
                    "shadow record thumbnail still unavailable at publish time",
                    metadata: [
                        "host": "\(URL(string: url)?.host ?? "unknown")",
                        "category": "\(String(describing: type(of: error)))",
                    ]
                )
            }
        }

        return (updated, count)
    }

    private func missingExternalThumbnailURL(in embed: JSONValue?) -> String? {
        guard case let .object(embed)? = embed,
              case let .string(type)? = embed["$type"],
              type == "app.bsky.embed.external",
              case let .object(external)? = embed["external"],
              external["thumb"] == nil,
              case let .string(uri)? = external["uri"]
        else { return nil }
        return uri
    }
}

public enum Timestamp {
    public static func iso8601(_ date: Date = Date()) -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    public static func normalized(_ string: String) -> String {
        date(from: string).map(iso8601) ?? string
    }

    public static func date(from string: String) -> Date? {
        let formatter = ISO8601DateFormatter()
        if let date = formatter.date(from: string) {
            return date
        }

        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: string)
    }
}
