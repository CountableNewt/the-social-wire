import Foundation

/// One finite direct-PDS check; third-party query providers never enter this authority path.
struct AuthoritativeContentCheck: Sendable {
  struct Scope: Sendable {
    let uri: String
    let did: String
    let collection: String
    let rkey: String

    init(_ uri: String) throws {
      guard uri.utf8.count <= 2048, let parsed = RenderFieldExtractor.parseAtUri(uri),
        ThinAppViewEnrollBackfill.isBackfillEligibleAuthorDid(parsed.did),
        ThinAppViewConfig.canonicalContentCollections.contains(parsed.collection),
        RepositoryMST.validPath(parsed.collection + "/" + parsed.rkey),
        uri == "at://\(parsed.did)/\(parsed.collection)/\(parsed.rkey)"
      else { throw AuthoritativeContentVerificationError.invalidScope }
      self.uri = uri; did = parsed.did; collection = parsed.collection; rkey = parsed.rkey
    }
  }

  struct Proof: Sendable {
    let present: Bool
    let did: String
    let pdsBase: String
    let cid: String?
    var fromCache = false
  }

  let store: any ThinAppViewStore
  let indexer: ThinAppViewIndexer
  let transport: any RepositorySnapshotHTTPTransport
  let plcURL: String
  var timeout: Duration = .seconds(8)

  func run(_ scope: Scope, cachedProof: Proof? = nil) async throws -> Proof {
    let deadline = ContinuousClock.now.advanced(by: timeout)
    return try await withThrowingTaskGroup(of: Proof.self) { group in
      group.addTask { try await perform(scope, cachedProof: cachedProof, deadline: deadline) }
      group.addTask {
        try await Task.sleep(until: deadline, clock: .continuous)
        throw AuthoritativeContentVerificationError.unavailable
      }
      defer { group.cancelAll() }
      guard let proof = try await group.next() else { throw CancellationError() }
      return proof
    }
  }

  private func perform(_ scope: Scope, cachedProof: Proof?, deadline: ContinuousClock.Instant) async throws -> Proof {
    try Task.checkCancellation()
    guard let metadataStore = store as? any ContentVerificationMetadataStoring else {
      throw AuthoritativeContentVerificationError.unavailable
    }
    let previous = try await metadataStore.contentVerificationMetadata(uri: scope.uri)
    if var cachedProof, cachedProof.present, cachedProof.did == scope.did,
      cachedProof.cid != nil, previous?.cid == cachedProof.cid {
      cachedProof.fromCache = true
      return cachedProof
    }
    let base = try await identity(scope.did, deadline: deadline)
    guard var url = URLComponents(string: base) else { throw AuthoritativeContentVerificationError.invalidResponse }
    url.path = "/xrpc/com.atproto.repo.getRecord"
    url.queryItems = [.init(name: "repo", value: scope.did), .init(name: "collection", value: scope.collection),
      .init(name: "rkey", value: scope.rkey)]
    guard let endpoint = url.url else { throw AuthoritativeContentVerificationError.invalidScope }
    let response = try await transport.get(url: endpoint, accept: "application/json",
      maximumBytes: 2 << 20, deadline: deadline)
    try Task.checkCancellation()
    let object = try ContentVerificationJSON.object(response.body, maximumBytes: 2 << 20)
    let present: Bool
    let cid: String?
    let json: Data
    if [400, 404].contains(response.status), object["error"] as? String == "RecordNotFound" {
      present = false; cid = nil
      json = Data("{}".utf8)
    } else if response.status == 200 {
      guard object["uri"] as? String == scope.uri, let value = object["value"] as? [String: Any],
        value["$type"] as? String == scope.collection, let returnedCID = object["cid"] as? String
      else { throw AuthoritativeContentVerificationError.invalidResponse }
      json = try JSONSerialization.data(withJSONObject: value)
      try PublicContentRecordCID.verify(json: json, cid: returnedCID)
      present = true; cid = returnedCID
    } else { throw AuthoritativeContentVerificationError.unavailable }
    // A migrated DID must not turn a response from its former PDS into a current deletion proof.
    guard try await identity(scope.did, deadline: deadline) == base else {
      throw AuthoritativeContentVerificationError.invalidResponse
    }
    try Task.checkCancellation()
    if present {
      guard try await metadataStore.contentVerificationMetadata(uri: scope.uri) == previous else {
        throw AuthoritativeContentVerificationError.unavailable
      }
      try await indexer.handleCommit(repoDid: scope.did, collection: scope.collection, rkey: scope.rkey,
        cid: cid ?? "", recordJSON: json, operation: "create", pdsBase: base,
        ingestionSource: "pds_entry_verification", contentExpectation: .init(cid: previous?.cid))
    } else if let previous {
      let deleted = try await indexer.applyAuthoritativeContentDeletion(repoDid: scope.did,
        collection: scope.collection, rkey: scope.rkey, expectedCID: previous.cid,
        publicationSite: previous.publicationSite)
      if !deleted, try await metadataStore.contentVerificationMetadata(uri: scope.uri) != nil {
        throw AuthoritativeContentVerificationError.unavailable
      }
    } else if try await metadataStore.contentVerificationMetadata(uri: scope.uri) != nil {
      throw AuthoritativeContentVerificationError.unavailable
    }
    try Task.checkCancellation()
    return Proof(present: present, did: scope.did, pdsBase: base, cid: cid)
  }

  private func identity(_ did: String, deadline: ContinuousClock.Instant) async throws -> String {
    let target: URL
    if did.hasPrefix("did:plc:") {
      guard let base = PDSResolvedEndpointValidator.validatedBase(plcURL, policy: .publicHTTPS),
        let url = URL(string: base + "/" + did) else { throw AuthoritativeContentVerificationError.invalidScope }
      target = url
    } else {
      let parts = did.dropFirst(8).split(separator: ":", omittingEmptySubsequences: false)
      guard let first = parts.first,
        let base = PDSResolvedEndpointValidator.validatedBase("https://" + first, policy: .publicHTTPS),
        var url = URL(string: base) else { throw AuthoritativeContentVerificationError.invalidScope }
      if parts.count == 1 { url.appendPathComponent(".well-known") }
      for part in parts.dropFirst() {
        guard let decoded = part.removingPercentEncoding, !decoded.isEmpty, decoded != ".", decoded != "..",
          !decoded.contains(where: { "/?#\\".contains($0) }) else { throw AuthoritativeContentVerificationError.invalidScope }
        url.appendPathComponent(decoded)
      }
      url.appendPathComponent("did.json")
      target = url
    }
    let response = try await transport.get(url: target, accept: "application/json", maximumBytes: 65_536, deadline: deadline)
    guard response.status == 200 else { throw AuthoritativeContentVerificationError.unavailable }
    let document = try ContentVerificationJSON.object(response.body, maximumBytes: 65_536)
    guard document["id"] as? String == did, let services = document["service"] as? [[String: Any]] else {
      throw AuthoritativeContentVerificationError.invalidResponse
    }
    let candidates = services.filter { $0["type"] as? String == "AtprotoPersonalDataServer" }
    guard candidates.count == 1, let service = candidates.first,
      ["#atproto_pds", did + "#atproto_pds"].contains(service["id"] as? String ?? ""),
      let endpoint = service["serviceEndpoint"] as? String,
      let base = PDSResolvedEndpointValidator.validatedBase(endpoint, policy: .publicHTTPS)
    else { throw AuthoritativeContentVerificationError.invalidResponse }
    return base
  }
}
