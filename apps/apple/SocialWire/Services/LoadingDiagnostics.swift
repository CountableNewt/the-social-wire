import CryptoKit
import Foundation

/// Debug-only timings. Never log request headers, bodies, URL paths, or queries.
enum LoadingDiagnostics {
    nonisolated static func resource(_ url: URL) -> String {
        let digest = SHA256.hash(data: Data(url.absoluteString.utf8))
        let identifier = digest.prefix(6).map { String(format: "%02x", $0) }.joined()
        return "\(url.host ?? "unknown")#\(identifier)"
    }

    nonisolated static func milliseconds(since start: TimeInterval) -> Int {
        Int((ProcessInfo.processInfo.systemUptime - start) * 1_000)
    }

    nonisolated static func log(_ message: @autoclosure () -> String) {
        #if DEBUG
        print("[loading-perf] \(message())")
        #endif
    }

    nonisolated static func failure(_ error: any Error) -> String {
        let error = error as NSError
        return "\(error.domain):\(error.code)"
    }
}
