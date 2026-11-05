import Foundation

/// Match the serving budgets used by the active Gateway contract, including sparse read scans.
enum AppViewProxyBudget {
  static func seconds(path: String, query: String?) -> Int64 {
    if path == "/v1/appview/enroll" || path == "/xrpc/app.thesocialwire.appview.enrollSources" {
      return 300
    }
    guard path == "/v1/appview/feed" || path == "/xrpc/app.thesocialwire.appview.getFeed" else {
      return 60
    }
    let items = URLComponents(string: "https://query.invalid/?\(query ?? "")")?.queryItems ?? []
    let filter = items.first { $0.name == "filter" }?.value
    let kind = items.first { $0.name == "kind" }?.value
    return (filter == "read" || filter == "unread") && kind != "list" ? 21 : 3
  }
}
