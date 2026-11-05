struct SocialDraftDownload: Sendable {
  let asset: SocialDraftAsset
  let file: SocialDraftTemporaryFile
  // The admission slot belongs to the response lifetime, including a body never consumed.
  let lease: SocialDraftAdmissionLease
}
