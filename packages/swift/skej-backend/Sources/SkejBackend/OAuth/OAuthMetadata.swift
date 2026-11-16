import SkejKit

enum OAuthMetadata {
    static func metadata(config: AppConfig) -> [String: JSONValue] {
        [
            "client_id": .string(config.clientID),
            "client_name": .string("The Social Wire Scheduling"),
            "client_uri": .string(config.publicOrigin),
            "application_type": .string("web"),
            "grant_types": .array([.string("authorization_code"), .string("refresh_token")]),
            "response_types": .array([.string("code")]),
            "redirect_uris": .array([.string(config.redirectURI)]),
            "scope": .string(AppConfig.scopes.joined(separator: " ")),
            "token_endpoint_auth_method": .string("none"),
            "dpop_bound_access_tokens": .bool(true),
        ]
    }
}
