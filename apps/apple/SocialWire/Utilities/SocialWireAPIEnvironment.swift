import Foundation

/// Base URL for the deployed Go API and OAuth metadata.
enum SocialWireAPIEnvironment {
    private static let productionBaseURLString = "https://api.thesocialwire.app"
    private static let testingBaseURLString = "https://api.testing.thesocialwire.app"

    /// Debug simulators/devices and explicit `Beta` builds hit the testing API.
    /// TestFlight and App Store archives use `Release` and production.
    static var baseURLString: String {
        #if DEBUG || SOCIALWIRE_TESTING_API
        testingBaseURLString
        #else
        productionBaseURLString
        #endif
    }

    static var baseURL: URL {
        guard let url = URL(string: baseURLString), url.scheme == "https", url.host != nil else {
            preconditionFailure("Invalid Social Wire API base URL.")
        }
        return url
    }

    /// Public sharing pages must target the same deployment as the API.
    static var webBaseURLString: String {
        #if DEBUG || SOCIALWIRE_TESTING_API
        "https://testing.thesocialwire.app"
        #else
        "https://thesocialwire.app"
        #endif
    }

    /// Discoverable OAuth **`client_id`** (**`GET .../ios-client-metadata.json`**).
    static var iosClientMetadataURL: URL {
        baseURL.appendingPathComponent("ios-client-metadata.json")
    }

    /// Native tool workflows remain unreleased until their client implementation is ready.
    /// Enable deliberately when the native release is ready; no runtime preference can bypass this gate.
    static let toolsFeaturesReleased = false

    /// Delegated Accounts remains independently disabled when other tools are released.
    static var brandAccountsEnabled: Bool {
        #if SOCIALWIRE_BRAND_ACCOUNTS_ENABLED
        return true
        #else
        return false
        #endif
    }

}
