struct GIFProviderRecord: Decodable, Sendable {
  let id: String?
  let title: String?
  let content_description: String?
  let media_formats: [String: GIFProviderRendition]?
}
