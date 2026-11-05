public enum IndexingWorkerPhase: String, Sendable {
  case starting, running, standby, restarting, stopping
}
