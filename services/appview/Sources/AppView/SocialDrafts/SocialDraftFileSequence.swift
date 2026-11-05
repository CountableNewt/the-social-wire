import Foundation
import NIOCore

struct SocialDraftFileSequence: AsyncSequence, Sendable {
  typealias Element = ByteBuffer
  let file: SocialDraftTemporaryFile
  func makeAsyncIterator() -> Iterator { Iterator(file: file) }

  struct Iterator: AsyncIteratorProtocol {
    let file: SocialDraftTemporaryFile
    private var handle: FileHandle?
    init(file: SocialDraftTemporaryFile) { self.file = file }
    mutating func next() async throws -> ByteBuffer? {
      try Task.checkCancellation()
      if handle == nil { handle = try FileHandle(forReadingFrom: file.url) }
      guard let bytes = try handle?.read(upToCount: 64 << 10), !bytes.isEmpty else {
        try handle?.close()
        return nil
      }
      return ByteBuffer(bytes: bytes)
    }
  }
}
