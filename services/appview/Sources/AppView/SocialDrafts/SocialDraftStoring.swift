import NIOCore

protocol SocialDraftStoring: Sendable {
  func reserve(owner: String, input: SocialDraftReserveInput) async throws -> SocialDraftAsset
  func upload<Body: AsyncSequence & Sendable>(owner: String, id: String, body: Body, contentLength: Int64) async throws -> SocialDraftAsset where Body.Element == ByteBuffer
  func download(owner: String, id: String) async throws -> SocialDraftDownload
  func manifest(owner: String, id: String) async throws -> SocialDraftManifest
  func putManifest(owner: String, input: SocialDraftPutManifestInput, deleted: Bool) async throws -> SocialDraftManifest
  func articleDraft(owner: String, id: String) async throws -> ArticleDraftDocument
  func articleDrafts(owner: String, cursor: String?, limit: Int) async throws -> ArticleDraftPage
  func putArticleDraft(owner: String, input: ArticleDraftPutInput) async throws -> ArticleDraftDocument
  func deleteArticleDraft(owner: String, input: ArticleDraftDeleteInput) async throws -> ArticleDraftDocument
  func runCleanup() async
}

extension SocialDraftStoring {
  func putManifest(owner: String, input: SocialDraftPutManifestInput) async throws -> SocialDraftManifest {
    try await putManifest(owner: owner, input: input, deleted: false)
  }
}

extension SocialDraftService: SocialDraftStoring {}
