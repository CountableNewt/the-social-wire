import Foundation

enum RepositorySnapshotValidator {
  static func commit(
    raw: Data, cid: RepositoryCID, did: String, revision: String,
    key: RepositorySigningKey, now: Date
  ) throws -> RepositoryCID {
    try cid.verify(raw)
    guard cid.isDAGCBOR, case .map(var fields) = try RepositoryDAGCBOR.decode(raw),
      Set(fields.keys) == ["did", "version", "rev", "prev", "data", "sig"],
      fields["did"]?.text == did, fields["version"]?.number == 3,
      fields["rev"]?.text == revision, let root = fields["data"]?.cid, root.isDAGCBOR,
      let signature = fields["sig"]?.data
    else { throw RepositorySnapshotError.invalid("invalid commit") }
    _ = try RepositoryMST.child(fields["prev"])
    try validateRevision(revision, now: now)
    fields["sig"] = nil
    try key.verify(signature: signature, message: RepositoryDAGCBOR.encode(.map(fields)))
    return root
  }

  static func records(
    root: RepositoryCID, did: String, collections: Set<String>, blocks: [RepositoryCID: Data],
    maximumRecords: Int = 20_000, maximumDepth: Int = 64, maximumBlocks: Int = 100_000
  ) throws -> [RepositorySnapshotRecord] {
    var tree = RepositoryMST(maximumDepth: maximumDepth, maximumRecords: maximumBlocks)
    let leaves = try tree.walk(root: root, blocks: blocks)
    let relevant = leaves.filter {
      collections.contains(String($0.path.split(separator: "/", maxSplits: 1)[0]))
    }
    guard relevant.count <= maximumRecords else {
      throw RepositorySnapshotError.invalid("record limit exceeded")
    }
    return try relevant.map { leaf in
      guard let data = blocks[leaf.cid] else {
        throw RepositorySnapshotError.invalid("missing record block")
      }
      try leaf.cid.verify(data)
      let value = try RepositoryDAGCBOR.decode(data)
      guard case .map = value else {
        throw RepositorySnapshotError.invalid("invalid record object")
      }
      return RepositorySnapshotRecord(
        uri: "at://\(did)/\(leaf.path)", cid: leaf.cid.string,
        value: try JSONSerialization.data(
          withJSONObject: value.jsonObject(), options: [.sortedKeys]))
    }
  }

  static func validateRevision(_ revision: String, now: Date) throws {
    let alphabet = Array("234567abcdefghijklmnopqrstuvwxyz".utf8)
    guard revision.utf8.count == 13 else {
      throw RepositorySnapshotError.invalid("invalid revision")
    }
    var value: UInt64 = 0
    for character in revision.utf8 {
      guard let digit = alphabet.firstIndex(of: character), value <= UInt64.max >> 5 else {
        throw RepositorySnapshotError.invalid("invalid revision")
      }
      value = value << 5 | UInt64(digit)
    }
    guard Double(value >> 10) / 1_000_000 <= now.timeIntervalSince1970 + 300 else {
      throw RepositorySnapshotError.invalid("future revision")
    }
  }
}
