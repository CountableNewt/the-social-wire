import AsyncHTTPClient
import Crypto
import Foundation
import Logging
import NIOCore
import OperationsCore

struct OperationsWebhookDelivery: Sendable {
  let url: String
  let secret: String
  let httpClient: HTTPClient
  let logger: Logger

  func deliver(_ alert: OperationsAlert) async throws {
    var lastError: Error?
    for attempt in 0..<5 {
      do {
        try await deliverOnce(alert)
        return
      } catch {
        lastError = error
        guard attempt < 4 else { break }
        let delayMilliseconds = 250 * (1 << attempt)
        try? await Task.sleep(for: .milliseconds(delayMilliseconds))
      }
    }
    throw lastError ?? WebhookDeliveryError.rejected
  }

  private func deliverOnce(_ alert: OperationsAlert) async throws {
    let body = try JSONEncoder().encode(alert)
    let signature = HMAC<SHA256>.authenticationCode(
      for: body,
      using: SymmetricKey(data: Data(secret.utf8))
    ).map { String(format: "%02x", $0) }.joined()
    var request = HTTPClientRequest(url: url)
    request.method = .POST
    request.headers.add(name: "Content-Type", value: "application/json")
    request.headers.add(name: "X-Social-Wire-Signature", value: "sha256=\(signature)")
    request.body = .bytes(ByteBuffer(data: body))
    let response = try await httpClient.execute(request, timeout: .seconds(15))
    guard (200..<300).contains(Int(response.status.code)) else {
      logger.warning("Operations alert webhook rejected", metadata: ["status_class": .string("non_2xx")])
      throw WebhookDeliveryError.rejected
    }
  }
}

enum WebhookDeliveryError: Error { case rejected }
