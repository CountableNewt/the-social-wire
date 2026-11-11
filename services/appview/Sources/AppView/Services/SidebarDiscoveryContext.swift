import Foundation
import GatewayCore

enum SidebarBuildPhase: Sendable {
  case full
  case priority
  case manualRefresh
  case folderPublications
}

struct SidebarDiscoveryContext: Sendable {
  let viewerDid: String
  let folders: [PublicationFolderRecord]
  let prefs: [PublicationPrefsRecordDTO]
  let subscribed: [ProjectionDiscoveredRow]
  let myPublications: [ProjectionDiscoveredRow]
  let unfoldered: [ProjectionDiscoveredRow]
  var following: [ProjectionDiscoveredRow]
  let uniqueRows: [ProjectionDiscoveredRow]
  let enrollAuthorDids: [String]
  let prefsByPublicationId: [String: PublicationPrefsRecordDTO]
}
