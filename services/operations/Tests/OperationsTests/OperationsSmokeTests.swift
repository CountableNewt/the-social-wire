import Foundation
import Testing

@testable import Operations

@Test("operations package resolves")
func operationsPackageResolves() {
  #expect(true)
}

@Test("Jetstream reconnect request does not require an operator reason")
func reconnectRequestDoesNotRequireReason() throws {
  let request = try JSONDecoder().decode(ReconnectJetstreamRequest.self, from: Data("{}".utf8))

  #expect(request.environmentConfirmation == nil)
}
