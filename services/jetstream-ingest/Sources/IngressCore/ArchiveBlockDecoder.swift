import CZstd
import Foundation

public enum ArchiveBlockDecoder {
  public static let maxBlockBytes = 256 << 20
  public static func decompress(
    _ frame: Data, maximumBytes: Int = maxBlockBytes, dictionary: Data? = nil
  ) throws -> Data {
    guard let context = ZSTD_createDStream() else {
      throw IngressError.malformedArchive("Cannot initialize zstd decoder")
    }
    defer { ZSTD_freeDStream(context) }
    guard ZSTD_isError(ZSTD_initDStream(context)) == 0,
      ZSTD_isError(ZSTD_DCtx_setParameter(context, ZSTD_d_windowLogMax, 28)) == 0
    else { throw IngressError.malformedArchive("Cannot bound zstd decoder") }
    if let dictionary {
      let loaded = dictionary.withUnsafeBytes {
        ZSTD_DCtx_loadDictionary(context, $0.baseAddress, $0.count)
      }
      guard ZSTD_isError(loaded) == 0 else {
        throw IngressError.malformedArchive("Invalid live compression dictionary")
      }
    }
    return try frame.withUnsafeBytes { source in
      var input = ZSTD_inBuffer(src: source.baseAddress, size: source.count, pos: 0)
      var output = Data()
      var complete = false
      repeat {
        let previous = input.pos
        var chunk = Data(count: 64 << 10)
        var written = 0
        let result = chunk.withUnsafeMutableBytes { destination in
          var target = ZSTD_outBuffer(dst: destination.baseAddress, size: destination.count, pos: 0)
          let value = ZSTD_decompressStream(context, &target, &input)
          written = target.pos
          return value
        }
        guard ZSTD_isError(result) == 0, written <= maximumBytes - output.count else {
          throw IngressError.malformedArchive("Corrupt or oversized zstd archive frame")
        }
        output.append(chunk.prefix(written))
        if result == 0 {
          complete = true
          break
        }
        guard input.pos > previous || written > 0 else {
          throw IngressError.malformedArchive("Truncated zstd archive frame")
        }
      } while true
      guard complete, input.pos == input.size else {
        throw IngressError.malformedArchive("Trailing zstd archive data")
      }
      return output
    }
  }
  public static func decodeCompressed(_ frame: Data) throws -> [IngressEvent] {
    try decode(decompress(frame))
  }
  public static func decode(_ block: Data) throws -> [IngressEvent] {
    var reader = BinaryCursor(block)
    let count = Int(try reader.little(4))
    guard count <= 1 << 18, count <= reader.remaining / 34 else {
      throw IngressError.malformedArchive("Invalid archive event count")
    }
    let sequence = try (0..<count).map { _ in try reader.little(8) }
    let witnessed = try (0..<count).map { _ in Int64(bitPattern: try reader.little(8)) }
    let indexed = try (0..<count).map { _ in Int64(bitPattern: try reader.little(8)) }
    let kinds = try reader.read(count)
    let collectionLengths = try reader.read(count).map(Int.init)
    let didLengths = try (0..<count).map { _ in Int(try reader.little(2)) }
    let keyLengths = try reader.read(count).map(Int.init)
    let revLengths = try reader.read(count).map(Int.init)
    let payloadLengths = try (0..<count).map { _ in Int(try reader.little(4)) }
    let collections = try collectionLengths.map { try reader.text($0) }
    let dids = try didLengths.map { try reader.text($0) }
    let keys = try keyLengths.map { try reader.text($0) }
    let revs = try revLengths.map { try reader.text($0) }
    let payloads = try payloadLengths.map { Data(try reader.read($0)) }
    guard reader.remaining == 0 else {
      throw IngressError.malformedArchive("Trailing archive block data")
    }
    return try (0..<count).map { index in
      guard sequence[index] > 0, sequence[index] <= UInt64(Int64.max) else {
        throw IngressError.invalidEvent("Archive cursor outside BIGINT")
      }
      let kind: String
      let value: JSONValue
      switch kinds[index] {
      case 1, 2, 3, 7:
        kind = "commit"
        var record: [String: JSONValue] = [
          "operation": .string(
            kinds[index] == 3 ? "delete" : kinds[index] == 2 ? "update" : "create"),
          "collection": .string(collections[index]), "rkey": .string(keys[index]),
          "rev": .string(revs[index]),
        ]
        if kinds[index] != 3 {
          let decoded = try DAGCBOR.decode(payloads[index])
          guard case .object = decoded else {
            throw IngressError.malformedArchive("Archive record is not a map")
          }
          record["record"] = decoded
          record["cid"] = .string(DAGCBOR.cid(for: payloads[index]))
        }
        value = .object(record)
      case 4, 5, 6:
        kind = kinds[index] == 4 ? "identity" : kinds[index] == 5 ? "account" : "sync"
        value = IngressEvent.lifecyclePayload(try DAGCBOR.decode(payloads[index]), kind: kind)
      default: throw IngressError.malformedArchive("Unknown archive event kind")
      }
      return try IngressEvent(
        payload: .object([
          "cursor": .integer(Int64(sequence[index])),
          "time_us": .integer(indexed[index] == 0 ? witnessed[index] : indexed[index]),
          "kind": .string(kind), "did": .string(dids[index]), kind: value,
        ]))
    }
  }
}
