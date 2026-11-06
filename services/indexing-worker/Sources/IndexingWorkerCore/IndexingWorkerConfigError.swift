public enum IndexingWorkerConfigError: Error, Sendable, Equatable, CustomStringConvertible {
  case invalidRole(String)
  case invalidPositiveInteger(String)
  case invalidPositiveSeconds(String)
  case duplicateHealthPort
  case invalidLeaseTiming

  public var description: String {
    switch self {
    case .invalidRole(let role):
      "INDEXING_WORKER_ROLE must be projection or coordinator; received '\(role)'."
    case .invalidPositiveInteger(let key): "\(key) must be a positive port number."
    case .invalidPositiveSeconds(let key): "\(key) must be a positive number of seconds."
    case .duplicateHealthPort: "Public and component health ports must be distinct."
    case .invalidLeaseTiming: "Role lease renewal must be shorter than the lease duration."
    }
  }
}
