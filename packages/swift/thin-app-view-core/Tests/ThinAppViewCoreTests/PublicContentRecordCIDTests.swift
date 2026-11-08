import Foundation
import Testing
@testable import ThinAppViewCore

@Suite struct PublicContentRecordCIDTests {
  @Test func verifiesGenericLinksBytesIntegersAndDocumentsAboveReadStateLimit() throws {
    let link = RepositoryCID.hash(Data([1, 2, 3]))
    let text = String(repeating: "a", count: 70_000)
    let object: [String: Any] = ["$type": "site.standard.document", "text": text,
      "blob": ["ref": ["$link": link.string], "bytes": ["$bytes": "AQID"]],
      "array": [true, NSNull(), -42] as [Any]]
    let expected: RepositoryCBORValue = .map(["$type": .string("site.standard.document"), "text": .string(text),
      "blob": .map(["ref": .link(link), "bytes": .bytes(Data([1, 2, 3]))]),
      "array": .array([.bool(true), .null, .negative(41)])])
    let cid = RepositoryCID.hash(RepositoryDAGCBOR.encode(expected)).string
    try PublicContentRecordCID.verify(json: JSONSerialization.data(withJSONObject: object), cid: cid)
    var modified = object
    modified["text"] = "Tampered"
    #expect(throws: PublicContentDiscoveryError.invalidResponse) {
      try PublicContentRecordCID.verify(json: JSONSerialization.data(withJSONObject: modified), cid: cid)
    }
  }

  @Test func enforcesPublicRecordByteAndDepthLimits() throws {
    let cid = RepositoryCID.hash(Data()).string
    #expect(throws: (any Error).self) { try PublicContentRecordCID.verify(json: Data(repeating: 32, count: (1 << 20) + 1), cid: cid) }
    var value: [String: Any] = ["a": "value"]
    for _ in 0..<33 { value = ["nested": value] }
    #expect(throws: (any Error).self) { try PublicContentRecordCID.verify(json: JSONSerialization.data(withJSONObject: value), cid: cid) }
  }
}
