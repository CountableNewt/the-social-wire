import Foundation

public actor IndexingWorkerLaneState {
  private var phases: [IndexingWorkerLane: IndexingWorkerPhase] = [:]
  private var stopping: [IndexingWorkerLane: Date] = [:]
  private var control: [IndexingWorkerLane: Date] = [:]

  public init() {}

  public func set(_ phase: IndexingWorkerPhase, for lane: IndexingWorkerLane, at: Date) {
    phases[lane] = phase
    if phase == .stopping {
      // operationStopped is still stopping until release. Do not restart the
      // watchdog grace period while a cancelled owner is draining.
      if stopping[lane] == nil { stopping[lane] = at }
    } else {
      stopping[lane] = nil
    }
  }

  public func record(_ event: IndexingWorkerLeaseEvent, for lane: IndexingWorkerLane, at: Date) {
    switch event {
    case .acquired:
      control[lane] = at
      set(.starting, for: lane, at: at)
    case .standby:
      control[lane] = at
      set(.standby, for: lane, at: at)
    case .renewed, .validated:
      control[lane] = at
    case .operationStarted:
      set(.running, for: lane, at: at)
    case .operationStopping, .operationStopped:
      set(.stopping, for: lane, at: at)
    case .acquisitionFailed, .validationFailed, .released, .releaseFailed:
      set(.restarting, for: lane, at: at)
    }
  }

  public func snapshot() -> [IndexingWorkerLane: IndexingWorkerPhase] { phases }

  public func hasRecentControlEvidence(
    lanes: [IndexingWorkerLane], at: Date, maximumAge: TimeInterval
  ) -> Bool {
    lanes.allSatisfy { lane in
      guard let observedAt = control[lane] else { return false }
      let age = at.timeIntervalSince(observedAt)
      return age >= 0 && age <= maximumAge
    }
  }

  public func unresponsiveLane(at: Date, grace: TimeInterval) -> IndexingWorkerLane? {
    IndexingWorkerLane.allCases.first { lane in
      guard let beganStopping = stopping[lane] else { return false }
      return at.timeIntervalSince(beganStopping) >= grace
    }
  }
}
