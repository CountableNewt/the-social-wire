public enum IndexingWorkerLane: String, CaseIterable, Sendable {
  case appView = "appview"
  case wire

  var coordinatorRole: String {
    switch self {
    case .appView: "indexing.appview-coordinator"
    case .wire: "indexing.wire-materializer"
    }
  }
}
