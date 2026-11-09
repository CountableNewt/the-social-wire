struct GIFAsset: Codable, Sendable {
  let id: String
  let title: String
  let description: String
  let url: String
  let poster: String
  let mp4: String?
  let webm: String?
  let width: Int
  let height: Int
  let attribution: String
}
