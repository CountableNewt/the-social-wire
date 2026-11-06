import AsyncHTTPClient
import Foundation
import SotoS3

final class SocialDraftS3Store: SocialDraftObjectStoring {
  private let client: AWSClient
  private let s3: S3
  private let bucket: String

  init(configuration: SocialDraftConfiguration, http: HTTPClient) {
    client = AWSClient(credentialProvider: .static(accessKeyId: configuration.accessKeyID,
      secretAccessKey: configuration.secretAccessKey), retryPolicy: .noRetry, httpClient: http)
    s3 = S3(client: client, region: Region(rawValue: configuration.region),
      endpoint: configuration.endpoint, timeout: .seconds(60), options: [.s3DisableChunkedUploads])
    bucket = configuration.bucket
  }

  func shutdown() async throws { try await client.shutdown() }

  func put(key: String, file: SocialDraftTemporaryFile, length: Int) async throws {
    let body = AWSHTTPBody(asyncSequence: SocialDraftFileSequence(file: file), length: length)
    _ = try await s3.putObject(.init(body: body, bucket: bucket, contentLength: Int64(length),
      contentType: "application/octet-stream", key: key))
  }

  func get(key: String, into file: SocialDraftTemporaryFile, maximumBytes: Int) async throws {
    let object = try await s3.getObject(.init(bucket: bucket, key: key))
    let handle = try FileHandle(forWritingTo: file.url)
    defer { try? handle.close() }
    var total = 0
    for try await bytes in object.body {
      try Task.checkCancellation()
      total += bytes.readableBytes
      guard total <= maximumBytes else { throw SocialDraftStorageError.unavailable }
      try handle.write(contentsOf: bytes.readableBytesView)
    }
  }

  func delete(key: String) async throws {
    _ = try await s3.with(timeout: .seconds(5)).deleteObject(.init(bucket: bucket, key: key))
  }
}
