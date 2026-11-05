public enum IndexingWorkerLeaseEvent: Sendable {
  case acquired, standby, renewed, validated
  case operationStarted, operationStopping, operationStopped
  case acquisitionFailed, validationFailed, released, releaseFailed
}
