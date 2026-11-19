import Foundation

/// NDJSON lines for `app.thesocialwire.appview.refreshFeed`, keyed by `kind` like bootstrap events.
enum FeedRefreshStreamEvent: Encodable, Sendable {
  case started(Started)
  case publication(Publication)
  case done(Done)

  struct Started: Encodable, Sendable {
    let total: Int
  }

  struct Publication: Encodable, Sendable {
    enum Status: String, Encodable, Sendable {
      /// New or changed articles were indexed.
      case updated
      /// The source answered with nothing new.
      case checked
      /// The source could not be reached; cached articles remain.
      case unavailable
    }

    let publicationId: String
    let status: Status
    let indexed: Int
    let completed: Int
    let total: Int
  }

  struct Done: Encodable, Sendable {
    let total: Int
    let indexed: Int
    let unavailable: Int
  }

  private enum Key: String, CodingKey {
    case kind, started, publication, done
  }

  func encode(to encoder: any Encoder) throws {
    var container = encoder.container(keyedBy: Key.self)
    switch self {
    case .started(let payload):
      try container.encode("started", forKey: .kind)
      try container.encode(payload, forKey: .started)
    case .publication(let payload):
      try container.encode("publication", forKey: .kind)
      try container.encode(payload, forKey: .publication)
    case .done(let payload):
      try container.encode("done", forKey: .kind)
      try container.encode(payload, forKey: .done)
    }
  }

  func encodedLine() throws -> Data {
    var data = try JSONEncoder().encode(self)
    data.append(0x0A)
    return data
  }
}
