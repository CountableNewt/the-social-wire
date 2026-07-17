import Foundation
import Logging

public actor OperationsTelemetryBuffer {
  public enum Signal: Sendable {
    case metric(OperationsMetricSample)
    case event(OperationsEvent)
    case span(TraceSpan)
  }

  private let store: any OperationsStore
  private let capacity: Int
  private let logger: Logger
  private var queue: [Signal] = []
  private var waiter: CheckedContinuation<Void, Never>?
  public private(set) var droppedCount = 0

  public init(store: any OperationsStore, capacity: Int = 4_096, logger: Logger) {
    self.store = store
    self.capacity = max(1, capacity)
    self.logger = logger
  }

  @discardableResult
  public func enqueue(_ signal: Signal) -> Bool {
    guard queue.count < capacity else {
      droppedCount += 1
      return false
    }
    queue.append(signal)
    waiter?.resume()
    waiter = nil
    return true
  }

  public func runForever() async {
    while !Task.isCancelled {
      guard let signal = next() else {
        await withCheckedContinuation { waiter = $0 }
        continue
      }
      do {
        switch signal {
        case .metric(let sample): try await store.recordMetric(sample)
        case .event(let event): try await store.recordEvent(event)
        case .span(let span): try await store.recordTraceSpan(span)
        }
      } catch {
        logger.warning(
          "Telemetry export failed; signal dropped",
          metadata: ["error_type": .string(OperationsRedactor.errorCategory(error))]
        )
      }
    }
  }

  public func pendingCount() -> Int { queue.count }

  private func next() -> Signal? {
    guard !queue.isEmpty else { return nil }
    return queue.removeFirst()
  }
}
