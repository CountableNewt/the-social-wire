public enum ReconciliationRetryError: Error, Equatable {
  case invalidScope
  case expectedCountMismatch
}
