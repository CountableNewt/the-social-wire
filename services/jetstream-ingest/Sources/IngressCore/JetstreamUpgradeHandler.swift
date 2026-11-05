import Foundation
import NIOCore
import NIOHTTP1

/// All mutable state is confined to this handler's channel event loop.
final class JetstreamUpgradeHandler: ChannelInboundHandler, RemovableChannelHandler {
  typealias InboundIn = HTTPClientResponsePart
  typealias OutboundOut = HTTPClientRequestPart
  let host: String
  let uri: String
  let promise: EventLoopPromise<Void>
  private var sent = false
  private var status: HTTPResponseStatus?
  private var body = Data()
  private(set) var completed = false
  init(host: String, uri: String, promise: EventLoopPromise<Void>) {
    self.host = host
    self.uri = uri
    self.promise = promise
  }
  func channelActive(context: ChannelHandlerContext) {
    send(context)
    context.fireChannelActive()
  }
  func handlerAdded(context: ChannelHandlerContext) {
    if context.channel.isActive { send(context) }
  }
  private func send(_ context: ChannelHandlerContext) {
    guard !sent else { return }
    sent = true
    let headers: HTTPHeaders = ["Host": host, "Sec-WebSocket-Protocol": "xrpc.v1.json"]
    context.write(
      wrapOutboundOut(
        .head(HTTPRequestHead(version: .http1_1, method: .GET, uri: uri, headers: headers))),
      promise: nil)
    context.writeAndFlush(wrapOutboundOut(.end(nil)), promise: nil)
  }
  func upgraded() {
    guard !completed else { return }
    completed = true
    promise.succeed(())
  }
  func timeout(channel: any Channel) {
    fail(IngressError.provider("WebSocket upgrade timed out"))
    channel.close(promise: nil)
  }
  func channelRead(context: ChannelHandlerContext, data: NIOAny) {
    switch unwrapInboundIn(data) {
    case .head(let head): status = head.status
    case .body(let buffer):
      guard buffer.readableBytes <= 4096 - body.count else {
        fail(IngressError.provider("Oversized WebSocket rejection"))
        context.close(promise: nil)
        return
      }
      body.append(contentsOf: buffer.readableBytesView)
    case .end:
      fail(Self.rejection(status: status?.code ?? 0, body: body))
      context.close(promise: nil)
    }
  }
  static func rejection(status: UInt, body: Data) -> IngressError {
    let envelope = try? JSONDecoder().decode(JSONValue.self, from: body)
    if status == 400 {
      if envelope?["error"]?.string == "FutureCursor" { return .sourceDiscontinuity }
      if envelope?["error"]?.string == "CursorTooOld" { return .cursorTooOld }
      if envelope?["error"]?.string == "UnknownZstdDictionary" { return .dictionaryRotated }
    }
    return .provider("WebSocket upgrade rejected (\(status))")
  }
  func channelInactive(context: ChannelHandlerContext) {
    fail(IngressError.provider("WebSocket channel closed before upgrade"))
    context.fireChannelInactive()
  }
  func errorCaught(context: ChannelHandlerContext, error: any Error) {
    fail(error)
    context.close(promise: nil)
  }
  private func fail(_ error: any Error) {
    guard !completed else { return }
    completed = true
    promise.fail(error)
  }
}
