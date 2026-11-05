import Foundation
import NIOCore
import NIOHTTP1
import NIOPosix
import NIOSSL
import NIOWebSocket
import WebSocketKit

/// Preserve the provider's pre-upgrade XRPC rejection, which WebSocketKit otherwise discards.
enum JetstreamSocket {
  static func connect(
    url: URL, group: any EventLoopGroup, onUpgrade: @escaping @Sendable (WebSocket) -> Void
  ) -> EventLoopFuture<Void> {
    let promise = group.next().makePromise(of: Void.self)
    guard let host = url.host, url.scheme == "wss" else {
      promise.fail(IngressError.invalidConfiguration("Live intake requires WSS"))
      return promise.futureResult
    }
    let bootstrap = ClientBootstrap(group: group).connectTimeout(.seconds(10)).channelInitializer {
      channel in
      do {
        let tls = try NIOSSLClientHandler(
          context: NIOSSLContext(configuration: .makeClientConfiguration()), serverHostname: host)
        try channel.pipeline.syncOperations.addHandler(tls)
        let request = JetstreamUpgradeHandler(
          host: host, uri: url.path + (url.query.map { "?" + $0 } ?? ""), promise: promise)
        let bound = NIOLoopBound(request, eventLoop: channel.eventLoop)
        let upgrader = NIOWebSocketClientUpgrader(
          maxFrameSize: 32 << 20, automaticErrorHandling: true
        ) { channel, _ in
          var configuration = WebSocket.Configuration()
          configuration.maxAccumulatedFrameCount = 4096
          configuration.maxAccumulatedFrameSize = 32 << 20
          return WebSocket.client(on: channel, config: configuration, onUpgrade: onUpgrade)
        }
        let upgrade: NIOHTTPClientUpgradeConfiguration = (
          [upgrader],
          { context in
            bound.value.upgraded()
            channel.pipeline.syncOperations.removeHandler(bound.value, promise: nil)
          }
        )
        try channel.pipeline.syncOperations.addHTTPClientHandlers(
          leftOverBytesStrategy: .forwardBytes, withClientUpgrade: upgrade)
        try channel.pipeline.syncOperations.addHandler(request)
        channel.eventLoop.scheduleTask(in: .seconds(10)) {
          if !bound.value.completed { bound.value.timeout(channel: channel) }
        }
        return channel.eventLoop.makeSucceededVoidFuture()
      } catch { return channel.eventLoop.makeFailedFuture(error) }
    }
    bootstrap.connect(host: host, port: url.port ?? 443).cascadeFailure(to: promise)
    return promise.futureResult
  }
}
