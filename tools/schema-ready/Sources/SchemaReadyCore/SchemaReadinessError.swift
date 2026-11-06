public enum SchemaReadinessError: Error, Equatable, Sendable {
  case invalidConfiguration(String)
  case timeout
}
