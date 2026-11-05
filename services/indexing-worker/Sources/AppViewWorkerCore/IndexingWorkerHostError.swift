public enum IndexingWorkerHostError: Error, Equatable {
  case missingDatabaseURL
  case inactiveWorkloadRequested(String)
}
