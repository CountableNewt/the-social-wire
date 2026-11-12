import Crypto
import Foundation
import SkejKit

extension EmbeddedSkejBackend {
    func reconcileLegacyQueue(did: String) async throws {
        guard let readLegacyQueue, try await !store.hasImportedLegacyScheduleQueue(did: did) else { return }
        let records = try await readLegacyQueue(did)
        let jobs = records.compactMap { legacyJob(did: did, rkey: $0.key, record: $0.value) }
        try await store.importLegacyScheduleQueue(did: did, jobs: jobs, now: Timestamp.iso8601())
    }

    func schedules(did: String) async throws -> [ScheduledPostSummary] {
        let records = try await authoritativeSchedules(did: did)
        try await repairEmbeddedQueue(did: did, records: records)
        let jobs = try await store.listScheduleJobs(did: did)
        return jobs.compactMap { job in
            guard let record = records[job.rkey] else { return nil }
            return summarize(job, record: record)
        }
    }

    func repairEmbeddedQueue(did: String) async throws {
        try await repairEmbeddedQueue(did: did, records: try await authoritativeSchedules(did: did))
    }

    private func authoritativeSchedules(did: String) async throws -> [String: SkejScheduleRecord] {
        if let pds = pdsClient as? ATProtoPDSClient {
            return try await pds.listAuthoritativeSchedules(did: did)
        }
        return try await pdsClient.listSchedules(did: did)
    }

    private func repairEmbeddedQueue(did: String, records: [String: SkejScheduleRecord]) async throws {
        for (rkey, record) in records {
            try await repairEmbeddedSubmission(did: did, rkey: rkey, record: record)
        }
    }

    func recoverPendingSubmission(now: Date = Date()) async {
        do {
            guard let submission = try await store.nextPendingScheduleSubmission(now: Timestamp.iso8601(now),
                retryAt: Timestamp.iso8601(now.addingTimeInterval(300)),
                expireBefore: Timestamp.iso8601(now.addingTimeInterval(-86_400))) else { return }
            guard let record = try await pdsClient.getScheduleForMutation(did: submission.did, rkey: submission.rkey) else { return }
            try await repairEmbeddedSubmission(did: submission.did, rkey: submission.rkey, record: record)
        } catch {
            logger.warning("Could not reconcile a pending schedule submission", metadata: [
                "category": "\(String(describing: type(of: error)))",
            ])
        }
    }

    private func repairEmbeddedSubmission(did: String, rkey: String, record: SkejScheduleRecord) async throws {
        guard rkey.hasPrefix("sw-"), rkey.utf8.count == 67,
              case .string(let originalKey) = record.unknownFields["socialWireSubmissionKey"],
              case .string(let proof) = record.unknownFields["socialWireSubmissionProof"],
              let data = Data(base64Encoded: proof),
              HMAC<SHA256>.isValidAuthenticationCode(data, authenticating: submissionData(did: did, rkey: rkey, originalKey: originalKey), using: submissionKey),
              var job = legacyJob(did: did, rkey: rkey, record: record)
        else { return }
        if (try? validateContent(record)) == nil && record.status == .scheduled {
            job.status = .publishing
            job.lastError = ScheduleError(code: .recordInvalid, message: "The schedule record could not be verified.")
        }
        try await store.insertScheduleJobIfMissing(job, now: Timestamp.iso8601())
    }

    private func submissionData(did: String, rkey: String, originalKey: String) -> Data {
        Data("\(did)\n\(rkey)\n\(originalKey)".utf8)
    }

    func create(did: String, input: SkejScheduleRecord) async throws -> ScheduledPostSummary {
        guard await createAdmission.acquire(did) else {
            throw APIError(status: .conflict, code: "ScheduleChanged", message: "This account is saving another schedule")
        }
        do {
            let result = try await createWithAdmission(did: did, input: input)
            await createAdmission.release(did)
            return result
        } catch {
            await createAdmission.release(did)
            throw error
        }
    }

    private func createWithAdmission(did: String, input: SkejScheduleRecord) async throws -> ScheduledPostSummary {
        try validateContent(input)
        guard input.status == .scheduled,
              let scheduledAt = Timestamp.date(from: input.scheduledAt), scheduledAt > Date()
        else { throw APIError(status: .badRequest, code: "InvalidSchedule", message: "Choose a future schedule time") }

        // The client's stable publish key identifies the submission even if the response is lost.
        let existing = try await matchingSchedule(did: did, publishRkey: input.publishRkey)
        if let existing, existing.record.status != .canceled {
            var expected = input
            prepare(&expected, did: did, now: existing.record.updatedAt, existing: existing.record)
            guard expected.scheduledAt == existing.record.scheduledAt,
                  expected.recordType == existing.record.recordType,
                  expected.posts == existing.record.posts,
                  expected.shadowRecord == existing.record.shadowRecord
            else { throw APIError(status: .conflict, code: "ScheduleChanged", message: "This content already has a different schedule") }
            return existing
        }

        let rkey = scheduleRecordKey(input.publishRkey, previousCancellation: existing?.rkey)
        let now = Timestamp.iso8601()
        var record = input
        prepare(&record, did: did, now: now, existing: nil)
        let proof = HMAC<SHA256>.authenticationCode(for: submissionData(did: did, rkey: rkey, originalKey: input.publishRkey), using: submissionKey)
        record.unknownFields["socialWireSubmissionProof"] = .string(Data(proof).base64EncodedString())
        try await store.rememberScheduleSubmission(did: did, rkey: rkey, now: now)
        record = try await writeCalendar(did: did, rkey: rkey, record: record)
        try await pdsClient.writeSchedule(did: did, rkey: rkey, record: record)
        let job = makeJob(did: did, rkey: rkey, record: record)
        try await store.insertScheduleJobIfMissing(job, now: now)
        return summarize(job, record: record)
    }

    func edit(did: String, rkey: String, update: SkejScheduleRecord?) async throws -> ScheduledPostSummary {
        guard validRecordKey(rkey), let job = try await store.scheduleJob(did: did, rkey: rkey)
        else { throw APIError(status: .notFound, code: "ScheduleNotFound", message: "Schedule not found") }
        let lease = UUID().uuidString
        let now = Date()
        let timestamp = Timestamp.iso8601(now)
        guard try await store.claimScheduleEdit(did: did, rkey: rkey, leaseID: lease,
            now: timestamp, expiresAt: Timestamp.iso8601(now.addingTimeInterval(300))) else { throw scheduleNotEditable() }
        do {
            guard let existing = try await pdsClient.getScheduleForMutation(did: did, rkey: rkey),
                  [.draft, .scheduled, .blocked, .failed].contains(existing.status), !hasPublishedEvidence(existing)
            else { throw scheduleNotEditable() }
            var record = existing
            if let update {
                guard update.publishRkey == existing.publishRkey, update.recordType == existing.recordType,
                      let scheduledAt = Timestamp.date(from: update.scheduledAt), scheduledAt > now
                else { throw APIError(status: .badRequest, code: "InvalidSchedule", message: "Choose a future time for this schedule") }
                record.scheduledAt = update.scheduledAt
                record.status = .scheduled
                if record.recordType == "site.standard.document", case .object(var document) = record.shadowRecord {
                    document["publishedAt"] = .string(update.scheduledAt)
                    if document["updatedAt"] != nil { document["updatedAt"] = .string(update.scheduledAt) }
                    record.shadowRecord = .object(document)
                }
                record.retry.nextAttemptAt = nil
                record.lastError = nil
            } else {
                record.status = .canceled
            }
            record.updatedAt = timestamp
            record = try await writeCalendar(did: did, rkey: rkey, record: record)
            try await pdsClient.writeSchedule(did: did, rkey: rkey, record: record)
            var projection = makeJob(did: did, rkey: rkey, record: record)
            projection.attempts = job.attempts
            try await store.upsertScheduleJob(projection, now: timestamp)
            try await store.releaseScheduleEdit(did: did, rkey: rkey, leaseID: lease)
            return summarize(projection, record: record)
        } catch {
            try? await store.releaseScheduleEdit(did: did, rkey: rkey, leaseID: lease)
            throw error
        }
    }

    private func matchingSchedule(did: String, publishRkey: String) async throws -> ScheduledPostSummary? {
        let scheduleKey = scheduleRecordKey(publishRkey, previousCancellation: nil)
        let matches = try await schedules(did: did).filter { summary in
            summary.rkey == scheduleKey || summary.record.publishRkey == publishRkey ||
                summary.record.unknownFields["socialWireSubmissionKey"] == .string(publishRkey)
        }
        let active = matches.filter { $0.record.status != .canceled }
        guard active.count <= 1 else {
            throw APIError(status: .conflict, code: "ScheduleChanged", message: "This content has multiple schedules")
        }
        return active.first ?? matches.max { $0.record.updatedAt < $1.record.updatedAt }
    }

    private func writeCalendar(did: String, rkey: String, record: SkejScheduleRecord) async throws -> SkejScheduleRecord {
        let calendar = CalendarCoordinator(pdsClient: pdsClient)
        let previous = try await calendar.event(did: did, rkey: rkey)
        let result = try await calendar.writeEventFirst(did: did, rkey: rkey, schedule: record, previous: previous)
        var projected = record
        projected.calendarEventUri = result.reference.uri
        projected.calendarEventCid = result.reference.cid
        return projected
    }
}

func legacyJob(did: String, rkey: String, record: SkejScheduleRecord) -> ScheduledJob? {
    guard validRecordKey(rkey), validRecordKey(record.publishRkey),
          record.type == "at.skej.schedule", Timestamp.date(from: record.scheduledAt) != nil,
          ["app.bsky.feed.post", "site.standard.document"].contains(record.recordType)
    else { return nil }
    var job = makeJob(did: did, rkey: rkey, record: record)
    let isRunnable = record.status == .scheduled || record.status == .blocked
    let uncertain = record.status == .publishing ||
        (isRunnable && (record.retry.attemptCount > 0 || hasPublishedEvidence(record)))
    if uncertain {
        job.status = .publishing
        job.lastError = ScheduleError(code: .unknown,
            message: "The previous publisher may have attempted this schedule. Review the published content before scheduling again.")
    }
    return job
}

private func makeJob(did: String, rkey: String, record: SkejScheduleRecord) -> ScheduledJob {
    ScheduledJob(did: did, rkey: rkey, scheduledAt: Timestamp.normalized(record.scheduledAt), status: record.status,
        attempts: record.retry.attemptCount, lastError: record.lastError, nextAttemptAt: record.retry.nextAttemptAt.map(Timestamp.normalized),
        lastAttemptAt: record.retry.lastAttemptAt, publishRkey: record.publishRkey, recordType: record.recordType,
        publishedUri: record.publishedUri, publishedCid: record.publishedCid,
        dependsOnScheduleUri: record.dependency?.dependsOnScheduleUri, parentPublishedUri: record.dependency?.parentPublishedUri)
}

private func summarize(_ job: ScheduledJob, record: SkejScheduleRecord) -> ScheduledPostSummary {
    ScheduledPostSummary(rkey: job.rkey, did: job.did, scheduleUri: ATURI.schedule(did: job.did, rkey: job.rkey),
        scheduledAt: record.scheduledAt, status: job.status, record: record, attempts: job.attempts,
        lastError: job.lastError, nextAttemptAt: job.nextAttemptAt,
        publishedUri: job.publishedUri ?? record.publishedUri, publishedCid: job.publishedCid ?? record.publishedCid)
}

private func hasPublishedEvidence(_ record: SkejScheduleRecord) -> Bool {
    record.publishedUri != nil || record.publishedCid != nil || !record.publishedPosts.isEmpty
}

private func scheduleNotEditable() -> APIError {
    APIError(status: .conflict, code: "ScheduleNotEditable", message: "This schedule is publishing, changed, or already finished")
}

func validRecordKey(_ value: String) -> Bool {
    value != "." && value != ".." &&
        value.range(of: #"^[a-zA-Z0-9._~:-]{1,512}$"#, options: .regularExpression) != nil
}

func scheduleRecordKey(_ publishKey: String, previousCancellation: String?) -> String {
    let value = previousCancellation.map { publishKey + "/" + $0 } ?? publishKey
    return "sw-" + SHA256.hash(data: Data(value.utf8)).map { String(format: "%02x", $0) }.joined()
}

private func validateContent(_ record: SkejScheduleRecord) throws {
    guard record.type == "at.skej.schedule", validRecordKey(record.publishRkey), !hasPublishedEvidence(record),
          (record.title?.count ?? 0) <= 120 else {
        throw APIError(status: .badRequest, code: "InvalidSchedule", message: "Invalid scheduled content")
    }
    let valid: Bool
    switch record.recordType {
    case "site.standard.document":
        if record.posts.isEmpty, case .object(let article) = record.shadowRecord,
           case .string(let title) = article["title"], !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
           case .string(let site) = article["site"],
           site.hasPrefix("https://") || site.contains("/site.standard.publication/"),
           article["$type"] == .string("site.standard.document"),
           article["publishedAt"] == .string(record.scheduledAt) { valid = true } else { valid = false }
    case "app.bsky.feed.post":
        valid = record.shadowRecord == nil && (1...100).contains(record.posts.count) &&
            record.posts.allSatisfy { !$0.text.isEmpty && $0.text.count <= 300 && $0.text.utf8.count <= 3000 }
    default:
        valid = false
    }
    guard valid else { throw APIError(status: .badRequest, code: "InvalidSchedule", message: "Invalid scheduled content") }
    do {
        for post in record.posts { try PostRecordCanonicalizer.validateExplicitFacets(in: post) }
    } catch {
        throw APIError(status: .badRequest, code: "InvalidSchedule", message: "Invalid post facets")
    }
}

private func prepare(_ record: inout SkejScheduleRecord, did: String, now: String, existing: SkejScheduleRecord?) {
    record.createdAt = existing?.createdAt ?? now
    record.updatedAt = now
    record.createdByDid = did
    record.approvedByDid = did
    record.approvedAt = existing?.approvedAt ?? now
    record.teamUri = nil
    record.calendarEventUri = nil
    record.calendarEventCid = nil
    record.lastError = nil
    record.retry = RetryState()
    record.unknownFields["socialWireSubmissionKey"] = .string(record.publishRkey)
    guard record.recordType == "app.bsky.feed.post" else { return }
    var assigned = Set<String>()
    record.posts = record.posts.enumerated().map { index, post in
        let canonical = PostRecordCanonicalizer.canonicalize(post)
        let existingKey = existing?.posts.indices.contains(index) == true ? existing?.posts[index].publishRkey : nil
        let candidate = existingKey ?? canonical.publishRkey
        let key = candidate.flatMap { ATProtoTID.isValid($0) && !assigned.contains($0) ? $0 : nil } ?? ATProtoTID.generate()
        assigned.insert(key)
        return PostPlan(text: canonical.text, source: canonical.source, publishRkey: key, facets: canonical.facets,
            reply: canonical.reply, embed: canonical.embed, langs: canonical.langs, labels: canonical.labels,
            tags: canonical.tags, unknownFields: canonical.unknownFields)
    }
    if let key = record.posts.first?.publishRkey { record.publishRkey = key }
}
