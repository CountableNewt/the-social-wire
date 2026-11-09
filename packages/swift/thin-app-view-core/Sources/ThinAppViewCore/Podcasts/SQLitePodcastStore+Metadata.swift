import Foundation
import GRDB

extension SQLitePodcastStore {
  public func updateMetadata(episode: PodcastEpisode, viewer: String?) async throws {
    if episode.visibility == "private" {
      guard let viewer else { throw PodcastStoreError.invalidRequest }
      let storage = try requirePrivateStorage()
      try await db.write { database in
        guard
          let ciphertext = try String.fetchOne(
            database,
            sql: "SELECT episode_data FROM podcast_private_episodes WHERE viewer_did=? AND id=?",
            arguments: [viewer, episode.id])
        else { return }
        var current = try JSONDecoder().decode(
          PodcastEpisode.self,
          from: Data(
            storage.open(ciphertext, viewer: viewer, entity: "episode", id: episode.id).utf8))
        guard current.id == episode.id, current.showId == episode.showId,
          current.audioUrl == episode.audioUrl, current.chapterSourceUrl == episode.chapterSourceUrl
        else { return }
        current.chapters = episode.chapters
        current.showArtworkUrl = episode.showArtworkUrl
        let clear = String(decoding: try JSONEncoder().encode(current), as: UTF8.self)
        let encrypted = try storage.seal(clear, viewer: viewer, entity: "episode", id: episode.id)
        try database.execute(
          sql:
            "UPDATE podcast_private_episodes SET episode_data=? WHERE viewer_did=? AND id=? AND episode_data=?",
          arguments: [encrypted, viewer, episode.id, ciphertext])
      }
    } else {
      guard !PodcastPrivateCatalog.isPrivateID(episode.id) else {
        throw PodcastStoreError.invalidRequest
      }
      let chapters = String(decoding: try JSONEncoder().encode(episode.chapters), as: UTF8.self)
      try await db.write { database in
        try database.execute(
          sql:
            "UPDATE podcast_episodes SET episode_json=json_set(episode_json,'$.chapters',json(?),'$.showArtworkUrl',?) WHERE id=? AND show_id=? AND json_extract(episode_json,'$.audioUrl')=? AND json_extract(episode_json,'$.chapterSourceUrl') IS ?",
          arguments: [
            chapters, episode.showArtworkUrl, episode.id, episode.showId, episode.audioUrl,
            episode.chapterSourceUrl,
          ])
      }
    }
  }
}
