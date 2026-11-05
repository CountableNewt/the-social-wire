import CZstd
import Foundation
import NIOConcurrencyHelpers
import NIOPosix
import WebSocketKit

/// The NIO callback boundary retains at most one bounded queue and closes on overflow.
public final class LiveConnection: Sendable {
  private struct State {
    var socket: WebSocket?
    var closed = false
  }
  private let state = NIOLockedValueBox(State())
  public init() {}
  private func attach(_ socket: WebSocket) {
    let close = state.withLockedValue { value in
      value.socket = socket
      return value.closed
    }
    if close { socket.close(promise: nil) }
  }
  public func close() {
    let socket = state.withLockedValue { value in
      value.closed = true
      return value.socket
    }
    socket?.close(promise: nil)
  }
  public func consume(
    configuration: IngressConfiguration, cursor: Int64, dictionary: Data? = nil,
    onEvent: @escaping @Sendable (IngressEvent) async throws -> Void
  ) async throws {
    var components = URLComponents()
    components.scheme = "wss"
    components.host = configuration.host
    components.path = "/xrpc/" + configuration.streamNSID
    components.queryItems =
      [URLQueryItem(name: "cursor", value: String(cursor))]
      + configuration.collections.map { URLQueryItem(name: "collections", value: $0) }
    if let dictionary {
      let id = dictionary.withUnsafeBytes { ZSTD_getDictID_fromDict($0.baseAddress, $0.count) }
      if id != 0 {
        components.queryItems?.append(URLQueryItem(name: "zstdDictionary", value: String(id)))
      }
    }
    guard let url = components.url else {
      throw IngressError.invalidConfiguration("Invalid live endpoint")
    }
    let (stream, continuation) = AsyncThrowingStream<Data, any Error>.makeStream(
      bufferingPolicy: .bufferingOldest(configuration.batchSize))
    continuation.onTermination = { [self] _ in close() }
    let group = MultiThreadedEventLoopGroup.singleton
    let future = JetstreamSocket.connect(url: url, group: group) { [self] socket in
      attach(socket)
      socket.onText { socket, text in
        if text.utf8.count > 32 << 20 {
          continuation.finish(
            throwing: IngressError.invalidEvent("Live frame exceeds safety limit"))
          socket.close(promise: nil)
          return
        }
        if case .dropped = continuation.yield(Data(text.utf8)) {
          continuation.finish(
            throwing: IngressError.provider(
              "Live intake queue full; reconnect from durable checkpoint"))
          socket.close(promise: nil)
        }
      }
      socket.onBinary { socket, buffer in
        guard let dictionary else {
          continuation.finish(
            throwing: IngressError.invalidEvent("Unexpected compressed live frame"))
          socket.close(promise: nil)
          return
        }
        do {
          let data = try ArchiveBlockDecoder.decompress(
            Data(buffer.readableBytesView), maximumBytes: 32 << 20, dictionary: dictionary)
          if case .dropped = continuation.yield(data) {
            continuation.finish(
              throwing: IngressError.provider(
                "Live intake queue full; reconnect from durable checkpoint"))
            socket.close(promise: nil)
          }
        } catch {
          continuation.finish(throwing: error)
          socket.close(promise: nil)
        }
      }
      socket.onClose.whenComplete { _ in continuation.finish() }
    }
    future.whenFailure { continuation.finish(throwing: $0) }
    try await withTaskCancellationHandler {
      defer { close() }
      try await future.get()
      for try await data in stream {
        try Task.checkCancellation()
        if let event = try IngressEvent.decodeLive(data) { try await onEvent(event) }
      }
      try Task.checkCancellation()
      throw IngressError.provider("Live subscription closed")
    } onCancel: { [self] in
      close()
      continuation.finish(throwing: CancellationError())
    }
  }
}
