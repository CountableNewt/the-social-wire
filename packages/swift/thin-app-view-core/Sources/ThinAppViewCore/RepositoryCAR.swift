import Foundation

enum RepositoryCAR {
  static func blocks(_ data: Data, requested: Set<RepositoryCID>, maximumBlocks: Int) throws
    -> [RepositoryCID: Data]
  {
    guard !data.isEmpty, data.count <= 8 * 1_024 * 1_024 else {
      throw RepositorySnapshotError.invalid("CAR size limit exceeded")
    }
    var cursor = RepositoryBinaryCursor(data)
    let headerLength = try cursor.varint()
    let header = try RepositoryDAGCBOR.decode(cursor.read(headerLength))
    guard header["version"]?.number == 1, let roots = header["roots"]?.values,
      roots.allSatisfy({ $0.cid != nil })
    else { throw RepositorySnapshotError.invalid("invalid CAR header") }
    var remaining = requested
    var blocks: [RepositoryCID: Data] = [:]
    while cursor.remaining > 0 {
      try Task.checkCancellation()
      let length = try cursor.varint()
      guard length >= 36 else { throw RepositorySnapshotError.invalid("invalid CAR section") }
      var section = RepositoryBinaryCursor(try cursor.read(length))
      let cid = try RepositoryCID(bytes: section.read(36))
      guard cid.isDAGCBOR else { throw RepositorySnapshotError.invalid("invalid CAR block codec") }
      let block = try section.read(section.remaining)
      guard remaining.remove(cid) != nil, blocks.count < maximumBlocks else {
        throw RepositorySnapshotError.invalid("unrequested or excessive CAR block")
      }
      try cid.verify(block)
      blocks[cid] = block
    }
    guard remaining.isEmpty else { throw RepositorySnapshotError.invalid("missing CAR blocks") }
    return blocks
  }
}
