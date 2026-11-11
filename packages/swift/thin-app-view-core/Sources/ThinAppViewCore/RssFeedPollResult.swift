public enum RssFeedPollResult: Sendable {
  case completed(indexed: Int, metadata: RssFeedFetchMetadata)
  case leaseBusy
  case backedOff(RssFeedFetchMetadata)
  case unfetchable
}
