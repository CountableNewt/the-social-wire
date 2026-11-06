import Testing
@testable import Gateway

@Test func sparseFeedScansRetainTheirServingBudget() {
  for path in ["/v1/appview/feed", "/xrpc/app.thesocialwire.appview.getFeed"] {
    #expect(AppViewProxyBudget.seconds(path: path, query: nil) == 3)
    #expect(AppViewProxyBudget.seconds(path: path, query: "filter=all") == 3)
    #expect(AppViewProxyBudget.seconds(path: path, query: "filter=unread") == 21)
    #expect(AppViewProxyBudget.seconds(path: path, query: "filter=read&kind=publication") == 21)
    #expect(AppViewProxyBudget.seconds(path: path, query: "filter=%75nread") == 21)
    #expect(AppViewProxyBudget.seconds(path: path, query: "filter=unread&kind=list") == 3)
  }
}

@Test func enrollmentHasTheSameBoundedAllowanceAsTheActiveGateway() {
  for path in ["/v1/appview/enroll", "/xrpc/app.thesocialwire.appview.enrollSources"] {
    #expect(AppViewProxyBudget.seconds(path: path, query: nil) == 300)
  }
  #expect(AppViewProxyBudget.seconds(path: "/v1/appview/entry", query: nil) == 60)
}
