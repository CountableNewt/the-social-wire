enum SocialDraftStorageError: Error, Equatable {
  case unavailable, invalid, notFound, conflict, assetRetired, quota, uploadPending
}
