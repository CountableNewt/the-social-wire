import Foundation
@testable import SkejKit
import Testing

@Test func calendarProjectionIsPublicMetadataOnlyAndReusesScheduleIdentity() throws {
    let schedule = SkejScheduleRecord(
        scheduledAt: "2026-09-01T15:00:00Z",
        title: "Launch announcement",
        createdAt: "2026-08-30T12:00:00Z",
        updatedAt: "2026-08-30T12:00:00Z",
        status: .draft,
        publishRkey: "3mcalendarcase",
        posts: [PostPlan(text: "Unpublished private draft body")]
    )

    let event = CalendarEventProjection.record(
        did: "did:plc:calendar",
        scheduleRkey: "3mcalendarcase",
        schedule: schedule
    )
    let encoded = try JSONEncoder().encode(event)
    let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])

    #expect(object["$type"] as? String == CalendarEventProjection.collection)
    #expect(object["name"] as? String == "Launch announcement")
    #expect(object["status"] as? String == CalendarEventStatus.planned.rawValue)
    #expect(object["startsAt"] as? String == schedule.scheduledAt)
    #expect(String(data: encoded, encoding: .utf8)?.contains("Unpublished private draft body") == false)
    #expect(event.uris.contains {
        $0.uri == "at://did:plc:calendar/at.skej.schedule/3mcalendarcase"
    })
}

@Test func calendarProjectionMarksTimingChangesAsRescheduled() {
    let schedule = SkejScheduleRecord(
        scheduledAt: "2026-09-02T15:00:00Z",
        createdAt: "2026-08-30T12:00:00Z",
        updatedAt: "2026-08-30T12:00:00Z",
        status: .scheduled,
        publishRkey: "3mrescheduled",
        posts: [PostPlan(text: "Body")]
    )
    let event = CalendarEventProjection.record(
        did: "did:plc:calendar",
        scheduleRkey: "3mrescheduled",
        schedule: schedule,
        rescheduled: true
    )
    #expect(event.status == .rescheduled)
}

@Test func calendarCoordinatorRejectsMissingAndPlannedEventsForPublication() async throws {
    let pds = InMemoryPDSClient()
    let coordinator = CalendarCoordinator(pdsClient: pds)
    await #expect(throws: CalendarReconciliationError.eventMissing) {
        _ = try await coordinator.requirePublishableEvent(
            did: "did:plc:calendar",
            rkey: "3mmissingevent"
        )
    }

    let schedule = SkejScheduleRecord(
        scheduledAt: "2026-09-02T15:00:00Z",
        createdAt: "2026-08-30T12:00:00Z",
        updatedAt: "2026-08-30T12:00:00Z",
        status: .draft,
        publishRkey: "3mplannedevent",
        posts: [PostPlan(text: "Body")]
    )
    _ = try await coordinator.writeEventFirst(
        did: "did:plc:calendar",
        rkey: "3mplannedevent",
        schedule: schedule
    )
    await #expect(throws: CalendarReconciliationError.eventNotPublishable(.planned)) {
        _ = try await coordinator.requirePublishableEvent(
            did: "did:plc:calendar",
            rkey: "3mplannedevent"
        )
    }
}
