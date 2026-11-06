import Foundation
import Testing

@testable import IndexingWorkerCore

@Suite("Indexing worker control evidence")
struct IndexingWorkerLaneStateTests {
  @Test func freshnessAndClockReversal() async {
    let state = IndexingWorkerLaneState()
    let now = Date(timeIntervalSince1970: 100)
    await state.record(.standby, for: .appView, at: now)
    #expect(
      await state.hasRecentControlEvidence(
        lanes: [.appView], at: now.addingTimeInterval(30), maximumAge: 30))
    #expect(
      !(await state.hasRecentControlEvidence(
        lanes: [.appView], at: now.addingTimeInterval(31), maximumAge: 30)))
    #expect(
      !(await state.hasRecentControlEvidence(
        lanes: [.appView], at: now.addingTimeInterval(-1), maximumAge: 30)))
    #expect(
      !(await state.hasRecentControlEvidence(lanes: [.appView, .wire], at: now, maximumAge: 30)))
  }

  @Test func stoppingDoesNotResetWatchdogGrace() async {
    let state = IndexingWorkerLaneState()
    let now = Date(timeIntervalSince1970: 100)
    await state.record(.operationStopping, for: .appView, at: now)
    await state.record(.operationStopped, for: .appView, at: now.addingTimeInterval(20))
    #expect(await state.unresponsiveLane(at: now.addingTimeInterval(29), grace: 30) == nil)
    #expect(await state.unresponsiveLane(at: now.addingTimeInterval(30), grace: 30) == .appView)
    await state.record(.released, for: .appView, at: now.addingTimeInterval(30))
    #expect(await state.unresponsiveLane(at: now.addingTimeInterval(40), grace: 30) == nil)
  }

  @Test func failedAcquisitionDoesNotCreateControlEvidence() async {
    let state = IndexingWorkerLaneState()
    let now = Date()
    await state.record(.acquisitionFailed, for: .appView, at: now)
    #expect(!(await state.hasRecentControlEvidence(lanes: [.appView], at: now, maximumAge: 30)))
    #expect(await state.snapshot()[.appView] == .restarting)
  }
}
