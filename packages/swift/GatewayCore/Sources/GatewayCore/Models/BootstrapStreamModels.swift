import Foundation

public enum AppViewBootstrapStreamEventKind: String, Codable, Sendable {
  case sidebarPriority
  case unreadCounts
  case selectedPublication
  case entriesPage
  case sidebarFolders
  case warning
  case error
  case done
}

public struct AppViewBootstrapUnreadCountsPayload: Codable, Sendable, Equatable {
  public let counts: [String: Int]

  public init(counts: [String: Int]) {
    self.counts = counts
  }
}

public struct AppViewBootstrapSelectedPublicationPayload: Codable, Sendable, Equatable {
  public let publicationId: String

  public init(publicationId: String) {
    self.publicationId = publicationId
  }
}

public struct AppViewBootstrapEntriesPagePayload: Codable, Sendable, Equatable {
  public let publicationId: String
  public let entries: [AppViewBootstrapEntryListItem]
  public let cursor: String?

  public init(publicationId: String, entries: [AppViewBootstrapEntryListItem], cursor: String?) {
    self.publicationId = publicationId
    self.entries = entries
    self.cursor = cursor
  }
}

public struct AppViewBootstrapEntryListItem: Codable, Sendable, Equatable {
  public let entryId: String
  public let title: String
  public let summary: String?
  public let publishedAt: Date
  public let thumbnailUrl: String?
  public let thumbnailFallbackUrl: String?

  public init(
    entryId: String,
    title: String,
    summary: String? = nil,
    publishedAt: Date,
    thumbnailUrl: String? = nil,
    thumbnailFallbackUrl: String? = nil
  ) {
    self.entryId = entryId
    self.title = title
    self.summary = summary
    self.publishedAt = publishedAt
    self.thumbnailUrl = thumbnailUrl
    self.thumbnailFallbackUrl = thumbnailFallbackUrl
  }
}

public struct AppViewBootstrapSidebarFoldersPayload: Codable, Sendable, Equatable {
  public let folderSections: [PublicationFolderSection]
  public let allPublicationRows: [SidebarPublicationRow]

  public init(folderSections: [PublicationFolderSection], allPublicationRows: [SidebarPublicationRow]) {
    self.folderSections = folderSections
    self.allPublicationRows = allPublicationRows
  }
}

public struct AppViewBootstrapMessagePayload: Codable, Sendable, Equatable {
  public let message: String

  public init(message: String) {
    self.message = message
  }
}

public struct AppViewBootstrapDonePayload: Codable, Sendable, Equatable {
  public let refreshedAt: Date

  public init(refreshedAt: Date) {
    self.refreshedAt = refreshedAt
  }
}

public struct AppViewBootstrapStreamEvent: Codable, Sendable {
  public let kind: AppViewBootstrapStreamEventKind
  public let sidebarPriority: PublicationSidebarResponse?
  public let unreadCounts: AppViewBootstrapUnreadCountsPayload?
  public let selectedPublication: AppViewBootstrapSelectedPublicationPayload?
  public let entriesPage: AppViewBootstrapEntriesPagePayload?
  public let sidebarFolders: AppViewBootstrapSidebarFoldersPayload?
  public let warning: AppViewBootstrapMessagePayload?
  public let error: AppViewBootstrapMessagePayload?
  public let done: AppViewBootstrapDonePayload?

  public init(
    kind: AppViewBootstrapStreamEventKind,
    sidebarPriority: PublicationSidebarResponse? = nil,
    unreadCounts: AppViewBootstrapUnreadCountsPayload? = nil,
    selectedPublication: AppViewBootstrapSelectedPublicationPayload? = nil,
    entriesPage: AppViewBootstrapEntriesPagePayload? = nil,
    sidebarFolders: AppViewBootstrapSidebarFoldersPayload? = nil,
    warning: AppViewBootstrapMessagePayload? = nil,
    error: AppViewBootstrapMessagePayload? = nil,
    done: AppViewBootstrapDonePayload? = nil
  ) {
    self.kind = kind
    self.sidebarPriority = sidebarPriority
    self.unreadCounts = unreadCounts
    self.selectedPublication = selectedPublication
    self.entriesPage = entriesPage
    self.sidebarFolders = sidebarFolders
    self.warning = warning
    self.error = error
    self.done = done
  }

  public static func sidebarPriority(_ response: PublicationSidebarResponse) -> Self {
    Self(kind: .sidebarPriority, sidebarPriority: response)
  }

  public static func unreadCounts(_ counts: [String: Int]) -> Self {
    Self(kind: .unreadCounts, unreadCounts: .init(counts: counts))
  }

  public static func selectedPublication(publicationId: String) -> Self {
    Self(kind: .selectedPublication, selectedPublication: .init(publicationId: publicationId))
  }

  public static func entriesPage(_ payload: AppViewBootstrapEntriesPagePayload) -> Self {
    Self(kind: .entriesPage, entriesPage: payload)
  }

  public static func sidebarFolders(_ payload: AppViewBootstrapSidebarFoldersPayload) -> Self {
    Self(kind: .sidebarFolders, sidebarFolders: payload)
  }

  public static func warning(_ message: String) -> Self {
    Self(kind: .warning, warning: .init(message: message))
  }

  public static func error(_ message: String) -> Self {
    Self(kind: .error, error: .init(message: message))
  }

  public static func done(refreshedAt: Date) -> Self {
    Self(kind: .done, done: .init(refreshedAt: refreshedAt))
  }
}

public enum AppViewBootstrapStreamNDJSON {
  public static func encodeLine(_ event: AppViewBootstrapStreamEvent) throws -> Data {
    let encoder = JSONEncoder()
    encoder.dateEncodingStrategy = .iso8601
    var data = try encoder.encode(event)
    data.append(0x0A)
    return data
  }
}
