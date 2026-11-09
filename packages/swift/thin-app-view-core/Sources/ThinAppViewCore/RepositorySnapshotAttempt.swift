import Foundation

actor RepositorySnapshotAttempt {
  private let reader: RepositorySnapshotReader
  private let deadline: ContinuousClock.Instant
  private var byteCount = 0, requests = 0
  private var blocks: [RepositoryCID: Data] = [:]

  init(reader: RepositorySnapshotReader, deadline: ContinuousClock.Instant) {
    self.reader = reader
    self.deadline = deadline
  }

  func read(did: String, collections: Set<String>) async throws -> RepositorySnapshot {
    let (base, key) = try await identity(did)
    try await active(did, base: base)
    let (cid, revision) = try await latest(did, base: base)
    try await fetchBlocks([cid], did: did, base: base)
    guard let commit = blocks[cid] else { throw RepositorySnapshotError.invalid("missing commit") }
    let root = try RepositorySnapshotValidator.commit(
      raw: commit, cid: cid, did: did, revision: revision,
      key: key, now: reader.now())
    try await loadTree(root: root, did: did, base: base)
    var tree = RepositoryMST(
      maximumDepth: reader.maximumDepth, maximumRecords: reader.maximumBlocks)
    let leaves = try tree.walk(root: root, blocks: blocks).filter {
      collections.contains(String($0.path.split(separator: "/", maxSplits: 1)[0]))
    }
    guard leaves.count <= reader.maximumRecords else {
      throw RepositorySnapshotError.invalid("record limit exceeded")
    }
    for start in stride(from: 0, to: leaves.count, by: 50) {
      try await fetchBlocks(
        leaves[start..<min(start + 50, leaves.count)].map(\.cid), did: did, base: base)
    }
    let records = try RepositorySnapshotValidator.records(
      root: root, did: did, collections: collections,
      blocks: blocks, maximumRecords: reader.maximumRecords, maximumDepth: reader.maximumDepth,
      maximumBlocks: reader.maximumBlocks)
    let (finalBase, finalKey) = try await identity(did)
    guard finalBase == base, finalKey == key else {
      throw RepositorySnapshotError.invalid("identity changed")
    }
    try await active(did, base: base)
    let (finalCID, finalRevision) = try await latest(did, base: base)
    guard finalCID == cid, finalRevision == revision else {
      throw RepositorySnapshotError.invalid("commit changed")
    }
    try Task.checkCancellation()
    return RepositorySnapshot(
      pdsBase: base, commitCID: cid.string, revision: revision, records: records)
  }

  private func get(_ url: URL, accept: String, limit: Int) async throws -> Data {
    for retry in 0...2 {
      try Task.checkCancellation()
      requests += 1
      guard requests <= reader.maximumRequests else {
        throw RepositorySnapshotError.invalid("request limit exceeded")
      }
      let response = try await reader.transport.get(
        url: url, accept: accept, maximumBytes: limit, deadline: deadline)
      byteCount += response.body.count
      guard response.body.count <= limit, byteCount <= reader.maximumBytes else {
        throw RepositorySnapshotError.invalid("byte limit exceeded")
      }
      if response.status == 429 && retry < 2 {
        let raw = Double(response.retryAfter ?? "") ?? 1
        let delay = raw.isFinite && raw >= 0 ? min(raw, 5) : 1
        try await Task.sleep(for: .seconds(delay))
        continue
      }
      guard response.status == 200 else {
        throw RepositorySnapshotError.invalid("upstream unavailable")
      }
      return response.body
    }
    throw RepositorySnapshotError.invalid("upstream unavailable")
  }

  private func query(_ base: String, path: String, values: [URLQueryItem]) throws -> URL {
    guard var url = URLComponents(string: base + path) else {
      throw RepositorySnapshotError.invalid("invalid URL")
    }
    url.queryItems = values
    guard let target = url.url else { throw RepositorySnapshotError.invalid("invalid URL") }
    return target
  }

  private func active(_ did: String, base: String) async throws {
    let url = try query(
      base, path: "/xrpc/com.atproto.sync.getRepoStatus", values: [.init(name: "did", value: did)])
    let data = try await get(url, accept: "application/json", limit: 65_536)
    let status = try JSONDecoder().decode(RepositorySnapshotStatus.self, from: data)
    guard status.did == did, status.active
    else {
      throw RepositorySnapshotError.invalid("inactive repository")
    }
  }

  private func latest(_ did: String, base: String) async throws -> (RepositoryCID, String) {
    let url = try query(
      base, path: "/xrpc/com.atproto.sync.getLatestCommit", values: [.init(name: "did", value: did)]
    )
    let data = try await get(url, accept: "application/json", limit: 65_536)
    guard let object = try JSONSerialization.jsonObject(with: data) as? [String: Any],
      let cid = object["cid"] as? String, let revision = object["rev"] as? String
    else {
      throw RepositorySnapshotError.invalid("invalid latest commit")
    }
    try RepositorySnapshotValidator.validateRevision(revision, now: reader.now())
    let commit = try RepositoryCID(cid)
    guard commit.isDAGCBOR else { throw RepositorySnapshotError.invalid("invalid commit codec") }
    return (commit, revision)
  }

  private func identity(_ did: String) async throws -> (String, RepositorySigningKey) {
    let target: URL
    if did.hasPrefix("did:plc:") {
      let identifier = did.dropFirst(8)
      guard identifier.count == 24,
        identifier.allSatisfy({ "abcdefghijklmnopqrstuvwxyz234567".contains($0) }),
        let base = PDSResolvedEndpointValidator.validatedBase(reader.plcBase, policy: .publicHTTPS),
        let url = URL(string: base + "/" + did)
      else { throw RepositorySnapshotError.invalid("invalid DID") }
      target = url
    } else if did.hasPrefix("did:web:") {
      let parts = did.dropFirst(8).split(separator: ":", omittingEmptySubsequences: false)
      guard let host = parts.first?.removingPercentEncoding,
        let base = PDSResolvedEndpointValidator.validatedBase(
          "https://" + host, policy: .publicHTTPS)
      else {
        throw RepositorySnapshotError.invalid("invalid web DID")
      }
      var segments: [String] = []
      for part in parts.dropFirst() {
        guard let decoded = part.removingPercentEncoding, !decoded.isEmpty, decoded != ".",
          decoded != "..",
          !decoded.contains(where: { "/?#\\".contains($0) })
        else { throw RepositorySnapshotError.invalid("invalid web DID path") }
        segments.append(decoded)
      }
      var url = URL(string: base)!
      if segments.isEmpty { url.appendPathComponent(".well-known") }
      for segment in segments { url.appendPathComponent(segment) }
      url.appendPathComponent("did.json")
      target = url
    } else {
      throw RepositorySnapshotError.invalid("unsupported DID")
    }
    let data = try await get(target, accept: "application/json", limit: 65_536)
    guard let document = try JSONSerialization.jsonObject(with: data) as? [String: Any],
      document["id"] as? String == did, let services = document["service"] as? [[String: Any]],
      let methods = document["verificationMethod"] as? [[String: Any]]
    else {
      throw RepositorySnapshotError.invalid("identity mismatch")
    }
    let pds = services.filter { $0["type"] as? String == "AtprotoPersonalDataServer" }
    guard pds.count == 1, let service = pds.first,
      ["#atproto_pds", did + "#atproto_pds"].contains(service["id"] as? String ?? ""),
      let raw = service["serviceEndpoint"] as? String,
      let base = PDSResolvedEndpointValidator.validatedBase(raw, policy: .publicHTTPS)
    else {
      throw RepositorySnapshotError.invalid("invalid PDS binding")
    }
    let keys = methods.filter {
      ["#atproto", did + "#atproto"].contains($0["id"] as? String ?? "")
    }
    guard keys.count == 1, let method = keys.first, method["controller"] as? String == did,
      method["type"] as? String == "Multikey", let key = method["publicKeyMultibase"] as? String
    else {
      throw RepositorySnapshotError.invalid("invalid signing key binding")
    }
    return (base, try RepositorySigningKey(multibase: key))
  }

  private func fetchBlocks(_ cids: [RepositoryCID], did: String, base: String) async throws {
    guard cids.allSatisfy(\.isDAGCBOR) else {
      throw RepositorySnapshotError.invalid("invalid block CID codec")
    }
    let requested = Set(cids.filter { blocks[$0] == nil })
    if requested.isEmpty { return }
    let values =
      [URLQueryItem(name: "did", value: did)]
      + requested.sorted { $0.string < $1.string }
      .map { URLQueryItem(name: "cids", value: $0.string) }
    let data = try await get(
      query(base, path: "/xrpc/com.atproto.sync.getBlocks", values: values),
      accept: "application/vnd.ipld.car", limit: 8 * 1_024 * 1_024)
    let fetched = try RepositoryCAR.blocks(
      data, requested: requested, maximumBlocks: reader.maximumBlocks - blocks.count)
    for (cid, raw) in fetched { blocks[cid] = raw }
  }

  private func loadTree(root: RepositoryCID, did: String, base: String) async throws {
    var queue: [(RepositoryCID, Int)] = [(root, 0)]
    var seen: Set<RepositoryCID> = [root]
    var offset = 0
    while offset < queue.count {
      let end = min(offset + 50, queue.count)
      let batch = Array(queue[offset..<end])
      offset = end
      try await fetchBlocks(batch.map(\.0), did: did, base: base)
      for (cid, depth) in batch {
        guard let raw = blocks[cid] else {
          throw RepositorySnapshotError.invalid("missing tree block")
        }
        let node = try RepositoryDAGCBOR.decode(raw)
        guard let entries = node["e"]?.values else {
          throw RepositorySnapshotError.invalid("invalid tree node")
        }
        let children =
          try [RepositoryMST.child(node["l"])] + entries.map { try RepositoryMST.child($0["t"]) }
        for child in children.compactMap({ $0 }) {
          guard depth < reader.maximumDepth, seen.insert(child).inserted,
            seen.count <= reader.maximumBlocks
          else {
            throw RepositorySnapshotError.invalid("repeated or excessive MST reference")
          }
          queue.append((child, depth + 1))
        }
      }
    }
  }
}
