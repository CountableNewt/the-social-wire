import Foundation

public struct TraceContext: Codable, Sendable {
  public let traceId: String
  public let spanId: String
  public let sampled: Bool

  public init(traceId: String = TraceContext.randomHex(bytes: 16), spanId: String = TraceContext.randomHex(bytes: 8), sampled: Bool = true) {
    self.traceId = traceId
    self.spanId = spanId
    self.sampled = sampled
  }

  public init?(traceparent: String) {
    let parts = traceparent.split(separator: "-")
    guard parts.count == 4,
          parts[0] == "00",
          parts[1].count == 32,
          parts[2].count == 16,
          let flags = UInt8(parts[3], radix: 16)
    else { return nil }
    let trace = String(parts[1])
    let span = String(parts[2])
    guard trace != String(repeating: "0", count: 32), span != String(repeating: "0", count: 16) else {
      return nil
    }
    self.traceId = trace
    self.spanId = span
    self.sampled = flags & 1 == 1
  }

  public var traceparent: String {
    "00-\(traceId)-\(spanId)-\(sampled ? "01" : "00")"
  }

  public func child(sampled: Bool? = nil) -> TraceContext {
    TraceContext(traceId: traceId, spanId: Self.randomHex(bytes: 8), sampled: sampled ?? self.sampled)
  }

  private static func randomHex(bytes: Int) -> String {
    (0..<bytes).map { _ in String(format: "%02x", UInt8.random(in: 0...255)) }.joined()
  }
}
