import Foundation

/// Receipt expiry is a scheduling hint only; the gateway still verifies the receipt.
struct GatewaySessionReceiptCache {
    private var session: AuthSession?
    private var receipt: String?
    private var expiresAt: Date?

    func value(for session: AuthSession, now: Date = Date()) -> String? {
        guard self.session == session, let expiresAt,
              expiresAt.timeIntervalSince(now) > 5 else { return nil }
        return receipt
    }

    mutating func store(_ receipt: String?, for session: AuthSession) {
        self = Self()
        guard let receipt,
              let body = receipt.split(separator: ".").first else { return }
        var base64 = String(body).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        base64 += String(repeating: "=", count: (4 - base64.count % 4) % 4)
        guard let data = Data(base64Encoded: base64),
              let claims = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let expiry = claims["exp"] as? Double else { return }
        self.session = session
        self.receipt = receipt
        expiresAt = min(Date(timeIntervalSince1970: expiry), session.expiresAt)
    }
}
