import Foundation
import Logging
import Testing
@testable import OperationsCore

@Suite("Operations telemetry buffer")
struct OperationsTelemetryBufferTests {
  @Test("drops sampled telemetry when the bounded queue is full")
  func overflowDropsWithoutBlocking() async throws {
    let path = FileManager.default.temporaryDirectory
      .appendingPathComponent("operations-buffer-\(UUID().uuidString).sqlite").path
    let logger = Logger(label: "operations-buffer-tests")
    let store = try SQLiteOperationsStore(path: path, environment: "dev", logger: logger)
    let buffer = OperationsTelemetryBuffer(store: store, capacity: 1, logger: logger)

    let first = await buffer.enqueue(.metric(.init(name: "socialwire.test", value: 1, dimensions: [:])))
    let second = await buffer.enqueue(.metric(.init(name: "socialwire.test", value: 2, dimensions: [:])))

    #expect(first)
    #expect(!second)
    #expect(await buffer.pendingCount() == 1)
    #expect(await buffer.droppedCount == 1)
  }

  @Test("an idle exporter stops promptly when cancelled")
  func idleExporterCancellation() async {
    let logger = Logger(label: "operations-buffer-cancellation-tests")
    let buffer = OperationsTelemetryBuffer(
      capacity: 1, logger: logger,
      exporter: { _ in })
    let task = Task { await buffer.runForever() }

    try? await Task.sleep(for: .milliseconds(10))
    let clock = ContinuousClock()
    let started = clock.now
    task.cancel()
    await task.value

    #expect(started.duration(to: clock.now) < .seconds(1))
  }
}
