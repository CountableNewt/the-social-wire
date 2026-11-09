import Foundation
import Testing
@testable import ThinAppViewCore

actor AuthoritativeContentTestTransport: RepositorySnapshotHTTPTransport {
  enum Mode: Sendable { case present, deleted, unavailable, empty404, wrongError, tampered, wrongURI, wrongType, wrongDID, unsafePDS, movedPDS, redirect, ambiguousError, stalled, delayed }
  let did = "did:plc:aaaaaaaaaaaaaaaaaaaaaaaa"
  let publication = "at://did:plc:aaaaaaaaaaaaaaaaaaaaaaaa/site.standard.publication/main"
  let mode: Mode
  private(set) var identityCalls = 0
  private(set) var recordCalls = 0
  var beforeSecondIdentity: (@Sendable () async throws -> Void)?

  init(_ mode: Mode = .present) { self.mode = mode }
  func setBeforeSecondIdentity(_ hook: @escaping @Sendable () async throws -> Void) { beforeSecondIdentity = hook }

  func get(url: URL, accept: String, maximumBytes: Int, deadline: ContinuousClock.Instant) async throws -> RepositorySnapshotHTTPResponse {
    #expect(url.scheme == "https" && accept == "application/json")
    #expect(deadline <= .now.advanced(by: .seconds(8)))
    if url.host == "plc.directory" {
      identityCalls += 1
      if identityCalls == 2 { try await beforeSecondIdentity?() }
      let endpoint = mode == .unsafePDS ? "http://127.0.0.1:1234" : mode == .movedPDS && identityCalls == 2 ? "https://moved.contentwire.net" : "https://pds.contentwire.net"
      return try response(200, ["id": mode == .wrongDID ? "did:plc:bbbbbbbbbbbbbbbbbbbbbbbb" : did,
        "service": [["id": "#atproto_pds", "type": "AtprotoPersonalDataServer", "serviceEndpoint": endpoint]]])
    }
    #expect(url.host == "pds.contentwire.net" && url.path == "/xrpc/com.atproto.repo.getRecord")
    let query = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
    #expect(query.first(where: { $0.name == "repo" })?.value == did)
    let key = query.first(where: { $0.name == "rkey" })?.value ?? ""
    recordCalls += 1
    if mode == .stalled { try await Task.sleep(for: .seconds(60)) }
    if mode == .delayed { try await Task.sleep(for: .milliseconds(80)) }
    switch mode {
    case .deleted, .movedPDS: return try response(400, ["error": "RecordNotFound", "message": "missing"])
    case .unavailable: return try response(503, ["error": "Unavailable"])
    case .empty404: return RepositorySnapshotHTTPResponse(status: 404, retryAfter: nil, body: Data())
    case .wrongError: return try response(404, ["error": "RepoNotFound"])
    case .redirect: return try response(302, ["error": "RecordNotFound"])
    case .ambiguousError: return RepositorySnapshotHTTPResponse(status: 400, retryAfter: nil, body: Data(#"{"error":"RepoNotFound","error":"RecordNotFound"}"#.utf8))
    default: break
    }
    var value = ["$type": "site.standard.document", "title": "Verified article", "site": publication,
      "publishedAt": "2026-11-05T00:00:00Z", "url": "https://publisher.contentwire.net/one"]
    let cid = RepositoryCID.hash(RepositoryDAGCBOR.encode(.map(value.mapValues { .string($0) }))).string
    if mode == .tampered { value["title"] = "Tampered after hashing" }
    if mode == .wrongType { value["$type"] = "site.standard.publication" }
    return try response(200, ["uri": "at://\(did)/site.standard.document/\(mode == .wrongURI ? "other" : key)", "cid": cid, "value": value])
  }

  private func response(_ status: Int, _ object: [String: Any]) throws -> RepositorySnapshotHTTPResponse {
    RepositorySnapshotHTTPResponse(status: status, retryAfter: nil, body: try JSONSerialization.data(withJSONObject: object))
  }
}
