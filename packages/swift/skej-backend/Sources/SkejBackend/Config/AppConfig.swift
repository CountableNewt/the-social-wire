import Foundation

struct AppConfig: Sendable {
    let publicOrigin: String
    let network: SkejATProtoNetwork

    var clientID: String { publicOrigin + "/oauth/scheduling/client-metadata.json" }
    var redirectURI: String { publicOrigin + "/oauth/scheduling/callback" }

    static let scopes = [
        "atproto",
        "repo:at.skej.schedule?action=create&action=update",
        "repo:community.lexicon.calendar.event?action=create&action=update",
        "repo:app.bsky.feed.post?action=create",
        "repo:site.standard.document?action=create",
        "blob:image/*",
    ]
}

struct SkejATProtoNetwork: Sendable {
    let plcOrigin = "https://plc.directory"

    func allowsOAuthEndpoint(_ value: String) -> Bool {
        guard let url = URL(string: value), url.scheme == "https", url.host != nil,
              url.user == nil, url.password == nil, url.fragment == nil
        else { return false }
        return true
    }

    func allowsPDS(_ value: String) -> Bool {
        Self.origin(value) == value.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }

    static func origin(_ value: String) -> String? {
        guard let url = URL(string: value), url.scheme == "https", url.host != nil,
              url.user == nil, url.password == nil, url.query == nil, url.fragment == nil,
              url.path.isEmpty || url.path == "/"
        else { return nil }
        return value.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }
}
