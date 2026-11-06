import Foundation
import HTTPTypes
import Hummingbird

struct AppViewEnrollmentError: HTTPResponseError, Encodable, Sendable {
  let error = "Service Unavailable"
  let message = "Enrollment unavailable"
  let articlesUpdated = true
  let sourceFailures: [AppViewEnrollmentSourceFailure]
  let unavailablePublicationIds: [String]

  var status: HTTPResponse.Status { .serviceUnavailable }

  init(_ partial: AppViewEnrollmentPartialFailure) {
    self.unavailablePublicationIds = Array(Set(partial.failures.map(\.publicationId))).sorted()
    let grouped = Dictionary(grouping: partial.failures) { $0.category + "\n" + $0.host }
    self.sourceFailures = grouped.keys.sorted().compactMap { key in
      guard let failures = grouped[key], let failure = failures.first else { return nil }
      return AppViewEnrollmentSourceFailure(category: failure.category, kind: "other", host: failure.host,
        count: Set(failures.map(\.publicationId)).count)
    }
  }

  func response(from request: Request, context: some RequestContext) throws -> Response {
    let data = try JSONEncoder().encode(self)
    return Response(status: status, headers: [.contentType: "application/json", .cacheControl: "no-store"],
      body: .init(byteBuffer: .init(data: data)))
  }
}
