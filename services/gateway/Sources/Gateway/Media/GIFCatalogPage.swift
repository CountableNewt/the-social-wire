struct GIFCatalogPage: Codable, Sendable {
  let results: [GIFAsset]
  let next: String
}
