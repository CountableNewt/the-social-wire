import Foundation

public enum CalendarReconciliationError: Error, Equatable, Sendable {
    case eventMissing
    case eventInvalid
    case eventNotPublishable(CalendarEventStatus)
}

public struct CalendarCoordinator: Sendable {
    private let pdsClient: any PDSClient

    public init(pdsClient: any PDSClient) {
        self.pdsClient = pdsClient
    }

    /// Writes the authoritative event before returning the schedule projection.
    public func writeEventFirst(
        did: String,
        rkey: String,
        schedule: SkejScheduleRecord,
        previous: CommunityCalendarEventRecord? = nil
    ) async throws -> (event: CommunityCalendarEventRecord, reference: CalendarEventReference) {
        let rescheduled = previous.map { $0.startsAt != schedule.scheduledAt } ?? false
        let event = CalendarEventProjection.record(
            did: did,
            scheduleRkey: rkey,
            schedule: schedule,
            rescheduled: rescheduled
        )
        let reference = try await pdsClient.writeCalendarEvent(
            did: did,
            rkey: rkey,
            record: event
        )
        return (event, reference)
    }

    public func event(did: String, rkey: String) async throws -> CommunityCalendarEventRecord? {
        try await pdsClient.getRecord(
            did: did,
            collection: CalendarEventProjection.collection,
            rkey: rkey,
            as: CommunityCalendarEventRecord.self
        )
    }

    public func requirePublishableEvent(
        did: String,
        rkey: String
    ) async throws -> CommunityCalendarEventRecord {
        guard let event = try await event(did: did, rkey: rkey) else {
            throw CalendarReconciliationError.eventMissing
        }
        let scheduleURI = ATURI.schedule(did: did, rkey: rkey)
        guard event.type == CalendarEventProjection.collection,
              Timestamp.date(from: event.startsAt) != nil,
              event.uris.contains(where: { $0.uri == scheduleURI })
        else {
            throw CalendarReconciliationError.eventInvalid
        }
        guard event.status == .scheduled || event.status == .rescheduled else {
            throw CalendarReconciliationError.eventNotPublishable(event.status)
        }
        return event
    }

    public func listEvents(did: String) async throws -> [CalendarEventSummary] {
        let records = try await pdsClient.listRecords(
            did: did,
            collection: CalendarEventProjection.collection,
            as: CommunityCalendarEventRecord.self
        )
        return records.map { rkey, record in
            CalendarEventSummary(
                accountDid: did,
                rkey: rkey,
                uri: ATURI.record(did: did, collection: CalendarEventProjection.collection, rkey: rkey),
                record: record
            )
        }
        .sorted { lhs, rhs in
            if lhs.record.startsAt == rhs.record.startsAt { return lhs.rkey < rhs.rkey }
            return lhs.record.startsAt < rhs.record.startsAt
        }
    }
}
