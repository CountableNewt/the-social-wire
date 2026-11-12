import Foundation

public enum CalendarEventStatus: String, Codable, CaseIterable, Sendable {
    case planned = "community.lexicon.calendar.event#planned"
    case scheduled = "community.lexicon.calendar.event#scheduled"
    case rescheduled = "community.lexicon.calendar.event#rescheduled"
    case postponed = "community.lexicon.calendar.event#postponed"
    case cancelled = "community.lexicon.calendar.event#cancelled"
}

public enum CalendarEventMode: String, Codable, Sendable {
    case virtual = "community.lexicon.calendar.event#virtual"
}

public struct CalendarEventURI: Codable, Equatable, Sendable {
    public var uri: String
    public var name: String?

    public init(uri: String, name: String? = nil) {
        self.uri = uri
        self.name = name
    }
}

/// Public metadata for a schedule. Post and thread bodies are deliberately absent.
public struct CommunityCalendarEventRecord: Codable, Equatable, Sendable {
    public let type: String
    public var name: String
    public var uris: [CalendarEventURI]
    public var startsAt: String
    public var endsAt: String?
    public var status: CalendarEventStatus
    public var mode: CalendarEventMode
    public var createdAt: String

    public init(
        type: String = "community.lexicon.calendar.event",
        name: String,
        uris: [CalendarEventURI],
        startsAt: String,
        endsAt: String? = nil,
        status: CalendarEventStatus,
        mode: CalendarEventMode = .virtual,
        createdAt: String
    ) {
        self.type = type
        self.name = name
        self.uris = uris
        self.startsAt = startsAt
        self.endsAt = endsAt
        self.status = status
        self.mode = mode
        self.createdAt = createdAt
    }

    enum CodingKeys: String, CodingKey {
        case type = "$type"
        case name
        case uris
        case startsAt
        case endsAt
        case status
        case mode
        case createdAt
    }
}

public struct CalendarEventReference: Codable, Equatable, Sendable {
    public var uri: String
    public var cid: String

    public init(uri: String, cid: String) {
        self.uri = uri
        self.cid = cid
    }
}

public struct CalendarEventSummary: Codable, Equatable, Sendable {
    public var accountDid: String
    public var rkey: String
    public var uri: String
    public var cid: String?
    public var record: CommunityCalendarEventRecord

    public init(
        accountDid: String,
        rkey: String,
        uri: String,
        cid: String? = nil,
        record: CommunityCalendarEventRecord
    ) {
        self.accountDid = accountDid
        self.rkey = rkey
        self.uri = uri
        self.cid = cid
        self.record = record
    }
}

public enum CalendarEventProjection {
    public static let collection = "community.lexicon.calendar.event"

    public static func record(
        did: String,
        scheduleRkey: String,
        schedule: SkejScheduleRecord,
        rescheduled: Bool = false
    ) -> CommunityCalendarEventRecord {
        let scheduleURI = ATURI.record(
            did: did,
            collection: "at.skej.schedule",
            rkey: scheduleRkey
        )
        let status: CalendarEventStatus
        switch schedule.status {
        case .draft:
            status = .planned
        case .canceled:
            status = .cancelled
        default:
            status = rescheduled ? .rescheduled : .scheduled
        }
        let startDate = Timestamp.date(from: schedule.scheduledAt)
        let endsAt = startDate.map { Timestamp.iso8601($0.addingTimeInterval(30 * 60)) }
        return CommunityCalendarEventRecord(
            name: schedule.title?.trimmingCharacters(in: .whitespacesAndNewlines).nilIfEmpty
                ?? "Scheduled post",
            uris: [
                CalendarEventURI(uri: scheduleURI, name: "Skej schedule"),
                CalendarEventURI(
                    uri: "https://skej.at/app/calendar?account=\(urlEncodeCalendar(did))&schedule=\(urlEncodeCalendar(scheduleRkey))",
                    name: "Open in Skej"
                ),
            ],
            startsAt: schedule.scheduledAt,
            endsAt: endsAt,
            status: status,
            createdAt: schedule.createdAt
        )
    }

    public static func isValidAuthoritativeEvent(
        _ event: CommunityCalendarEventRecord,
        scheduleURI: String
    ) -> Bool {
        guard event.type == collection,
              Timestamp.date(from: event.startsAt) != nil,
              event.uris.contains(where: { $0.uri == scheduleURI })
        else { return false }
        return event.status == .scheduled || event.status == .rescheduled
    }
}

private func urlEncodeCalendar(_ value: String) -> String {
    value.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? value
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}
