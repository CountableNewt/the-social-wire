import Foundation

/// Base URL and client credentials for L@tr / LatrKit gateway mutations (save/archive/delete enrichment).
enum LatrGatewayEnvironment {
    private static let localBaseURLString = "http://127.0.0.1:8080"
    private static let testBaseURLString = "https://api.testing.latr.link"
    private static let prodBaseURLString = "https://api.latr.link"
    private static let legacyDevBaseURLString = "https://latr-link-dev-gateway.fly.dev"
    private static let legacyProdBaseURLString = "https://latr-link-prod-gateway.fly.dev"

    static let clientIdHeaderName = "X-Latr-Client-Id"
    static let apiKeyHeaderName = "X-Latr-API-Key"
    static let officialClientHeaderName = "X-Latr-Official-Client"

    static var baseURLString: String {
        if let configured = ProcessInfo.processInfo.environment["LATR_GATEWAY_URL"]?.trimmingCharacters(in: .whitespacesAndNewlines),
           !configured.isEmpty {
            return configured.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        }

        #if DEBUG || SOCIALWIRE_TESTING_API
        return testBaseURLString
        #else
        return prodBaseURLString
        #endif
    }

    static var baseURL: URL {
        guard let url = URL(string: baseURLString) else {
            preconditionFailure("Invalid L@tr gateway base URL.")
        }
        return url
    }

    /// Split developer credentials from latrkit.dev (preferred for third-party clients).
    static var developerClientId: String? {
        trimmedEnv("LATR_GATEWAY_CLIENT_ID")
    }

    static var developerApiKey: String? {
        trimmedEnv("LATR_GATEWAY_API_KEY")
    }

    /// Base64 official client credential (`the-social-wire-web` in latr-gateway env). Legacy fallback.
    static var officialClientCredential: String? {
        trimmedEnv("LATR_GATEWAY_CLIENT_CREDENTIAL")
    }

    static var hasDeveloperCredentials: Bool {
        developerClientId != nil && developerApiKey != nil
    }

    private static func trimmedEnv(_ key: String) -> String? {
        let raw = ProcessInfo.processInfo.environment[key]?
            .trimmingCharacters(in: .whitespacesAndNewlines)
        guard let raw, !raw.isEmpty else { return nil }
        return raw
    }
}
