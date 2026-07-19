import Testing

@testable import OperationsCore

@Suite("OperationsRedactor")
struct OperationsRedactorTests {
  @Test("Credentials and record bodies are removed")
  func removesSensitiveValues() {
    let result = OperationsRedactor.boundedAttributes([
      "route": "/v1/appview/entries",
      "Authorization": "DPoP secret",
      "dpop_proof": "secret",
      "recordBody": "private",
    ])
    #expect(result == ["route": "/v1/appview/entries"])
  }

  @Test("Identifiers are hashed without exposing their source")
  func hashesIdentifiers() {
    let source = "at://did:plc:private/site.standard.document/secret"
    let hashed = OperationsRedactor.recordIdentifierHash(source)
    #expect(!hashed.contains("did:plc"))
    #expect(hashed.count == 24)
  }
}
