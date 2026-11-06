import Foundation
import Testing
@testable import Gateway

@Test func discardedResponseOwnersReleaseTheirTransferPermit() async throws {
  let admission = SocialDraftAdmission()
  let first = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
  let second = try await admission.acquire(owner: "did:plc:viewer", transfer: true)
  func discard() {
    let owner = PrivateDraftLease(admission: admission, lease: first)
    withExtendedLifetime(owner) {}
  }
  discard()
  let until = ContinuousClock.now.advanced(by: .seconds(2))
  var acquired: SocialDraftAdmission.Lease?
  while ContinuousClock.now < until {
    do { acquired = try await admission.acquire(owner: "did:plc:viewer", transfer: true); break }
    catch { try await Task.sleep(for: .milliseconds(10)) }
  }
  let replacement = try #require(acquired)
  await admission.release(second)
  await admission.release(replacement)
}

@Test func discardedPrivateGIFSpoolsRemoveTheirPrivateDirectory() throws {
  let directory = FileManager.default.temporaryDirectory.appendingPathComponent("gif-lifetime-\(UUID().uuidString)")
  try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: false, attributes: [.posixPermissions: 0o700])
  let file = directory.appendingPathComponent("media")
  #expect(FileManager.default.createFile(atPath: file.path, contents: Data("GIF89a".utf8), attributes: [.posixPermissions: 0o600]))
  func discard() {
    let owner = PrivateGIFFile(directory: directory, url: file, length: 6, mime: "image/gif")
    withExtendedLifetime(owner) {}
  }
  discard()
  #expect(!FileManager.default.fileExists(atPath: directory.path))
}
