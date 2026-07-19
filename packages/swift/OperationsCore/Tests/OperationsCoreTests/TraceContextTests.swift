import Testing

@testable import OperationsCore

@Suite("TraceContext")
struct TraceContextTests {
  @Test("W3C traceparent round trips")
  func roundTrips() throws {
    let context = TraceContext(
      traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
      spanId: "00f067aa0ba902b7",
      sampled: true
    )
    let parsed = try #require(TraceContext(traceparent: context.traceparent))
    #expect(parsed.traceId == context.traceId)
    #expect(parsed.spanId == context.spanId)
    #expect(parsed.sampled)
    #expect(parsed.child().traceId == context.traceId)
  }

  @Test("Invalid all-zero trace IDs are rejected")
  func rejectsInvalid() {
    #expect(
      TraceContext(
        traceparent: "00-00000000000000000000000000000000-0000000000000000-01"
      ) == nil
    )
  }
}
