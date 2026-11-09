actor PDSRecoveryShutdownProgress {
  var started = false
  var finished = false
  var closeCalls = 0
  var closedAfterFinish = false
  func start() { started = true }
  func finish() { finished = true }
  func close() { closeCalls += 1; closedAfterFinish = finished }
}
