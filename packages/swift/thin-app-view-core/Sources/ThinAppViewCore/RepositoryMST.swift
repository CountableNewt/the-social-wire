import Crypto
import Foundation

struct RepositoryMST {
  struct Leaf: Sendable {
    let path: String
    let cid: RepositoryCID
  }
  private(set) var leaves: [Leaf] = []
  private var visited: Set<RepositoryCID> = []
  private let maximumDepth: Int
  private let maximumRecords: Int

  init(maximumDepth: Int = 64, maximumRecords: Int = 100_000) {
    self.maximumDepth = maximumDepth
    self.maximumRecords = maximumRecords
  }

  mutating func walk(root: RepositoryCID, blocks: [RepositoryCID: Data]) throws -> [Leaf] {
    try walk(root, depth: 0, blocks: blocks)
    guard try Self.canonicalRoot(leaves) == root else {
      throw RepositorySnapshotError.invalid("noncanonical MST")
    }
    return leaves
  }

  private mutating func walk(_ cid: RepositoryCID, depth: Int, blocks: [RepositoryCID: Data]) throws
  {
    try Task.checkCancellation()
    guard depth <= maximumDepth, visited.insert(cid).inserted, visited.count <= maximumRecords,
      let data = blocks[cid]
    else { throw RepositorySnapshotError.invalid("invalid or incomplete MST") }
    try cid.verify(data)
    let node = try RepositoryDAGCBOR.decode(data)
    guard case .map(let fields) = node, Set(fields.keys) == ["e", "l"],
      let entries = node["e"]?.values
    else {
      throw RepositorySnapshotError.invalid("invalid MST node")
    }
    if let left = try Self.child(node["l"]) { try walk(left, depth: depth + 1, blocks: blocks) }
    var previous = Data()
    for entry in entries {
      guard case .map(let fields) = entry, Set(fields.keys) == ["p", "k", "v", "t"],
        let prefix = entry["p"]?.number, prefix <= UInt64(previous.count),
        let suffix = entry["k"]?.data, let value = entry["v"]?.cid, value.isDAGCBOR
      else {
        throw RepositorySnapshotError.invalid("invalid MST entry")
      }
      let pathBytes = Data(previous.prefix(Int(prefix))) + suffix
      guard let path = String(data: pathBytes, encoding: .utf8), Self.validPath(path),
        leaves.last.map({ $0.path.utf8.lexicographicallyPrecedes(path.utf8) }) ?? true
      else {
        throw RepositorySnapshotError.invalid("invalid MST ordering")
      }
      leaves.append(.init(path: path, cid: value))
      guard leaves.count <= maximumRecords else {
        throw RepositorySnapshotError.invalid("MST record limit")
      }
      previous = pathBytes
      if let right = try Self.child(entry["t"]) {
        try walk(right, depth: depth + 1, blocks: blocks)
      }
    }
  }

  static func child(_ value: RepositoryCBORValue?) throws -> RepositoryCID? {
    if case .null = value { return nil }
    guard let cid = value?.cid, cid.isDAGCBOR else {
      throw RepositorySnapshotError.invalid("invalid MST child")
    }
    return cid
  }

  static func validPath(_ path: String) -> Bool {
    let parts = path.split(separator: "/", omittingEmptySubsequences: false)
    guard parts.count == 2, parts[1].utf8.count <= 512, !parts[1].isEmpty,
      parts[1] != ".", parts[1] != "..",
      parts[1].utf8.allSatisfy({ byte in
        (48...57).contains(byte) || (65...90).contains(byte) || (97...122).contains(byte)
          || [46, 95, 126, 58, 45].contains(byte)
      })
    else { return false }
    let labels = parts[0].split(separator: ".", omittingEmptySubsequences: false)
    guard labels.count >= 3, parts[0].utf8.count <= 317 else { return false }
    return labels.enumerated().allSatisfy { index, label in
      guard !label.isEmpty, label.utf8.count <= 63,
        let first = label.utf8.first, let last = label.utf8.last,
        (65...90).contains(first) || (97...122).contains(first)
          || (index > 0 && index < labels.count - 1 && (48...57).contains(first)),
        last != 45
      else { return false }
      return label.utf8.allSatisfy { byte in
        (65...90).contains(byte) || (97...122).contains(byte)
          || (48...57).contains(byte) || (index < labels.count - 1 && byte == 45)
      }
    }
  }

  static func height(_ path: String) -> Int {
    var zeros = 0
    for byte in SHA256.hash(data: Data(path.utf8)) {
      if byte == 0 {
        zeros += 8
      } else {
        zeros += byte.leadingZeroBitCount
        break
      }
    }
    return zeros / 2
  }

  static func canonicalRoot(_ leaves: [Leaf]) throws -> RepositoryCID {
    let maxHeight = leaves.map { height($0.path) }.max() ?? 0
    return try node(leaves[...], height: maxHeight)
  }

  private static func node(_ leaves: ArraySlice<Leaf>, height: Int) throws -> RepositoryCID {
    guard height >= 0 else { throw RepositorySnapshotError.invalid("invalid MST height") }
    let positions = leaves.indices.filter { Self.height(leaves[$0].path) == height }
    if positions.isEmpty {
      let left: RepositoryCBORValue =
        leaves.isEmpty ? .null : .link(try node(leaves, height: height - 1))
      return RepositoryCID.hash(RepositoryDAGCBOR.encode(.map(["e": .array([]), "l": left])))
    }
    var entries: [RepositoryCBORValue] = []
    var previous = Data()
    let first = positions[0]
    let left: RepositoryCBORValue =
      first == leaves.startIndex
      ? .null
      : .link(try node(leaves[..<first], height: height - 1))
    for (offset, index) in positions.enumerated() {
      let leaf = leaves[index]
      let key = Data(leaf.path.utf8)
      let next = offset + 1 < positions.count ? positions[offset + 1] : leaves.endIndex
      let prefix = zip(previous, key).prefix { $0.0 == $0.1 }.count
      let right: RepositoryCBORValue =
        index + 1 == next
        ? .null
        : .link(try node(leaves[(index + 1)..<next], height: height - 1))
      entries.append(
        .map([
          "p": .unsigned(UInt64(prefix)), "k": .bytes(key.dropFirst(prefix)), "v": .link(leaf.cid),
          "t": right,
        ]))
      previous = key
    }
    return RepositoryCID.hash(RepositoryDAGCBOR.encode(.map(["e": .array(entries), "l": left])))
  }
}
