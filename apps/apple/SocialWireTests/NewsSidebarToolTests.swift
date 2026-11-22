import Testing
@testable import SocialWire

@Suite("Web sidebar tool parity")
struct NewsSidebarToolTests {
    @Test("Retained native tools preserve their names and order")
    func webToolOrder() {
        #expect(NewsSidebarTool.allCases.map(\.title) == [
            "Articles", "Live Post", "Livestream", "Delegated Accounts", "Content Calendar", "Analytics"
        ])
        #expect(Set(NewsSidebarTool.allCases.map(\.id)).count == 6)
    }

    @Test("Unreleased tools are hidden from the native sidebar")
    func unreleasedToolsHidden() {
        #expect(!SocialWireAPIEnvironment.toolsFeaturesReleased)
        #expect(NewsSidebarTool.availableTools.isEmpty)
    }

    @Test("Delegated Accounts requires both its feature flag and the tools release gate", arguments: [false, true])
    func brandAccountsRequiresBothGates(brandAccountsEnabled: Bool) {
        #expect(NewsSidebarTool.availableTools(
            toolsFeaturesReleased: false,
            brandAccountsEnabled: brandAccountsEnabled
        ).isEmpty)
        let released = NewsSidebarTool.availableTools(
            toolsFeaturesReleased: true,
            brandAccountsEnabled: brandAccountsEnabled
        )
        #expect(released.contains(.brandAccounts) == brandAccountsEnabled)
        #expect(released.filter { $0 != .brandAccounts } == [
            .articles, .livePost, .livestream, .contentCalendar, .analytics
        ])
    }

    @Test("Native Delegated Accounts requires an explicit build flag")
    func brandAccountsBuildFlag() {
        #if SOCIALWIRE_BRAND_ACCOUNTS_ENABLED
        #expect(SocialWireAPIEnvironment.brandAccountsEnabled)
        #else
        #expect(!SocialWireAPIEnvironment.brandAccountsEnabled)
        #endif
    }
}
