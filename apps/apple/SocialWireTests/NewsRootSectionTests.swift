import Testing
@testable import SocialWire

@Suite("Native Section Navigation")
struct NewsRootSectionTests {
    @Test("The native tab bar and sidebar share three ordered destinations")
    func sections() {
        #expect(NewsRootSection.allCases == [.readLater, .feeds, .lists])
        #expect(NewsRootSection(rawValue: "topics") == nil)
        #expect(NewsRootSection(rawValue: "social") == nil)
        #expect(NewsRootSection(rawValue: "media") == nil)
    }
}
