struct GIFProviderPage: Decodable, Sendable {
  let results: [GIFProviderRecord]?
  let next: String?
}
