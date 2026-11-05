import AsyncHTTPClient
import Foundation
import NIOConcurrencyHelpers
import NIOCore
import NIOHTTP1
import NIOPosix
import Testing

@testable import IngressCore

struct ArchiveFixture: Sendable {
  enum Scenario: Sendable { case truncated, stripes, throttle, generationChange }
  let scenario: Scenario
  let requests = NIOLockedValueBox<[String]>([])
  let data = Data("abcdefghijklmnopqrstuvwx".utf8)
  let checksum = "aabbccdd11223344"
}

final class ArchiveFixtureHandler: ChannelInboundHandler {
  typealias InboundIn = HTTPServerRequestPart
  typealias OutboundOut = HTTPServerResponsePart
  let fixture: ArchiveFixture
  init(_ fixture: ArchiveFixture) { self.fixture = fixture }
  func channelRead(context: ChannelHandlerContext, data: NIOAny) {
    guard case .head(let request) = unwrapInboundIn(data) else { return }
    let range = request.headers.first(name: "Range") ?? ""
    let count = fixture.requests.withLockedValue { values in
      values.append(range)
      return values.count
    }
    if fixture.scenario == .throttle && count == 1 {
      let headers: HTTPHeaders = ["Content-Length": "0", "Retry-After": "0"]
      context.write(
        wrapOutboundOut(
          .head(.init(version: .http1_1, status: .tooManyRequests, headers: headers))), promise: nil
      )
      context.writeAndFlush(wrapOutboundOut(.end(nil)), promise: nil)
      return
    }
    let bounds = range.dropFirst(6).split(separator: "-", omittingEmptySubsequences: false)
    let start = bounds.first.flatMap { Int($0) } ?? 0
    let end = bounds.count > 1 && !bounds[1].isEmpty ? Int(bounds[1])! : fixture.data.count - 1
    let length = end - start + 1
    let hash = fixture.scenario == .generationChange ? "deadbeef11223344" : fixture.checksum
    let headers: HTTPHeaders = [
      "Content-Length": String(length),
      "Content-Range": "bytes \(start)-\(end)/\(fixture.data.count)", "ETag": "\"\(hash)\"",
    ]
    context.write(
      wrapOutboundOut(.head(.init(version: .http1_1, status: .partialContent, headers: headers))),
      promise: nil)
    let bytes = fixture.data.subdata(in: start..<(end + 1))
    if fixture.scenario == .truncated && count == 1 {
      let bound = NIOLoopBound(context, eventLoop: context.eventLoop)
      context.writeAndFlush(wrapOutboundOut(.body(.byteBuffer(.init(bytes: bytes.prefix(7))))))
        .whenComplete { _ in bound.value.close(promise: nil) }
    } else {
      context.write(wrapOutboundOut(.body(.byteBuffer(.init(bytes: bytes)))), promise: nil)
      context.writeAndFlush(wrapOutboundOut(.end(nil)), promise: nil)
    }
  }
}

@Suite(.serialized)
struct ArchiveTransportTests {
  @Test func shortBodyResumesAtExactByteWithoutDroppingDownloadedPrefix() async throws {
    try await withFixture(.truncated, stripes: 1) { transport, fixture in
      let url = try await transport.segment(segment(fixture))
      defer { try? FileManager.default.removeItem(at: url) }
      #expect(try Data(contentsOf: url) == fixture.data)
      #expect(fixture.requests.withLockedValue { $0 } == ["bytes=0-", "bytes=7-"])
      #expect(await transport.rangeResumeCount == 1)
    }
  }
  @Test func parallelStripesReassembleIdenticalBytes() async throws {
    try await withFixture(.stripes, stripes: 3) { transport, fixture in
      let url = try await transport.segment(segment(fixture))
      defer { try? FileManager.default.removeItem(at: url) }
      #expect(try Data(contentsOf: url) == fixture.data)
      #expect(
        Set(fixture.requests.withLockedValue { $0 })
          == Set(["bytes=0-0", "bytes=0-7", "bytes=8-15", "bytes=16-23"]))
    }
  }
  @Test func retryAfterAndGenerationChangeKeepTheirDistinctBoundaries() async throws {
    try await withFixture(.throttle, stripes: 1) { transport, fixture in
      let url = try await transport.segment(segment(fixture))
      defer { try? FileManager.default.removeItem(at: url) }
      #expect(try Data(contentsOf: url) == fixture.data)
      #expect(await transport.retryCount == 1)
    }
    try await withFixture(.generationChange, stripes: 1) { transport, fixture in
      await #expect(throws: (any Error).self) { try await transport.segment(segment(fixture)) }
      #expect(fixture.requests.withLockedValue { $0.count } == 1)
    }
  }
  func segment(_ fixture: ArchiveFixture) -> SnapshotPlan.Segment {
    try! JSONDecoder().decode(
      SnapshotPlan.Segment.self,
      from: Data(
        "{\"name\":\"seg_1.jss\",\"index\":1,\"checksum\":\"\(fixture.checksum)\",\"minSeq\":1,\"maxSeq\":2,\"mode\":\"segment\"}"
          .utf8))
  }
  func withFixture(
    _ scenario: ArchiveFixture.Scenario, stripes: Int,
    operation: (ArchiveTransport, ArchiveFixture) async throws -> Void
  ) async throws {
    let fixture = ArchiveFixture(scenario: scenario)
    let server = try await ServerBootstrap(group: MultiThreadedEventLoopGroup.singleton)
      .childChannelInitializer { channel in
        channel.eventLoop.makeCompletedFuture {
          try channel.pipeline.syncOperations.configureHTTPServerPipeline()
          try channel.pipeline.syncOperations.addHandler(ArchiveFixtureHandler(fixture))
        }
      }.bind(host: "127.0.0.1", port: 0).get()
    let port = try #require(server.localAddress?.port)
    let config = try IngressConfiguration(environment: [
      "APP_ENV": "dev", "DATABASE_URL": "postgres://fixture/db", "JETSTREAM_API_KEY": "fixture",
      "JETSTREAM_SEGMENT_STRIPES": String(stripes), "JETSTREAM_BACKOFF_MIN": "1ms",
      "JETSTREAM_BACKOFF_MAX": "10ms",
    ])
    let client = HTTPClient(
      eventLoopGroupProvider: .singleton, configuration: .init(redirectConfiguration: .disallow))
    let budget = ReplayBudget(incidentLimit: 1000, dailyLimit: 2000)
    let transport = ArchiveTransport(
      config: config, client: client, budget: budget,
      origin: URL(string: "http://127.0.0.1:\(port)")!, recordUsage: { _ in })
    do {
      try await operation(transport, fixture)
      try await client.shutdown()
      try await server.close().get()
    } catch {
      try? await client.shutdown()
      try? await server.close().get()
      throw error
    }
  }
}
