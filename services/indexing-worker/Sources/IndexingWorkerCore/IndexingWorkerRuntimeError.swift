public enum IndexingWorkerRuntimeError: Error, Sendable, Equatable {
  case missingLaneFactory(IndexingWorkerLane)
  case noActiveLanes
  case coordinatorRequiresAuthority
  case componentExited(IndexingWorkerLane)
  case controlEvidenceUnavailable
  case laneUnavailable(IndexingWorkerLane)
  case laneNotRunning(IndexingWorkerLane, IndexingWorkerPhase?)
  case unresponsiveComponent(IndexingWorkerLane)
}
