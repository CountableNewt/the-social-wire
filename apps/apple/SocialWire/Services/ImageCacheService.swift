import Foundation
import ImageIO

/// Shared remote image cache with bounded in-memory retention and URLSession disk backing.
actor ImageCacheService {
    static let shared = ImageCacheService()
    private static let maximumSourceImageBytes: Int64 = 16 * 1024 * 1024

    private final class CacheEntry: NSObject {
        let image: PlatformImage

        init(_ image: PlatformImage) {
            self.image = image
        }
    }

    private let memoryCache = NSCache<NSString, CacheEntry>()
    private let download: @Sendable (URL) async throws -> (URL, URLResponse)

    private struct PendingDownload {
        let id: UUID
        let task: Task<Void, Never>
        var waiters: [UUID: CheckedContinuation<PlatformImage?, Never>]
    }

    private var pendingDownloads: [String: PendingDownload] = [:]
    private var failedUntil: [String: Date] = [:]
    private let failureCooldown: TimeInterval


    var pendingRequestCount: Int {
        pendingDownloads.values.reduce(0) { $0 + $1.waiters.count }
    }

    init(failureCooldown: TimeInterval = 30, download: (@Sendable (URL) async throws -> (URL, URLResponse))? = nil) {
        self.failureCooldown = max(0, failureCooldown)
        memoryCache.countLimit = 160
        memoryCache.totalCostLimit = 48 * 1024 * 1024

        let urlCache = URLCache(
            memoryCapacity: 16 * 1024 * 1024,
            diskCapacity: 256 * 1024 * 1024,
            diskPath: "socialwire-image-cache"
        )
        let configuration = URLSessionConfiguration.default
        configuration.urlCache = urlCache
        configuration.requestCachePolicy = .returnCacheDataElseLoad
        let session = URLSession(configuration: configuration)
        self.download = download ?? { try await session.download(from: $0) }
    }

    func image(for url: URL, maxPixelSize: CGFloat) async -> PlatformImage? {
        guard !Task.isCancelled else { return nil }
        let key = cacheKey(url: url, maxPixelSize: maxPixelSize)
        if let cached = memoryCache.object(forKey: key as NSString)?.image {
            LoadingDiagnostics.log("image memory-hit resource=\(LoadingDiagnostics.resource(url)) pixels=\(Int(maxPixelSize))")
            return cached
        }

        if let retryAt = failedUntil[key], retryAt > Date() {
            LoadingDiagnostics.log("image cooldown resource=\(LoadingDiagnostics.resource(url)) pixels=\(Int(maxPixelSize))")
            return nil
        }
        failedUntil.removeValue(forKey: key)
        let waiterID = UUID()
        return await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                guard !Task.isCancelled else {
                    continuation.resume(returning: nil)
                    return
                }
                if pendingDownloads[key] != nil {
                    LoadingDiagnostics.log("image joined resource=\(LoadingDiagnostics.resource(url)) pixels=\(Int(maxPixelSize))")
                    pendingDownloads[key]?.waiters[waiterID] = continuation
                    return
                }
                let downloadID = UUID()
                let task = Task {
                    let image = await downloadImage(for: url, maxPixelSize: maxPixelSize)
                    finishDownload(key: key, id: downloadID, image: image, cancelled: Task.isCancelled)
                }
                pendingDownloads[key] = PendingDownload(
                    id: downloadID,
                    task: task,
                    waiters: [waiterID: continuation]
                )
            }
        } onCancel: {
            Task { await self.cancelRequest(key: key, waiterID: waiterID) }
        }
    }

    private func cancelRequest(key: String, waiterID: UUID) {
        guard let continuation = pendingDownloads[key]?.waiters.removeValue(forKey: waiterID) else { return }
        continuation.resume(returning: nil)
        // A disappearing row must not cancel a download still needed by another row.
        if pendingDownloads[key]?.waiters.isEmpty == true {
            pendingDownloads.removeValue(forKey: key)?.task.cancel()
        }
    }

    private func finishDownload(key: String, id: UUID, image: PlatformImage?, cancelled: Bool) {
        // A cancelled download can finish after a new request for the same key starts.
        guard pendingDownloads[key]?.id == id,
              let pending = pendingDownloads.removeValue(forKey: key) else { return }
        if let image {
            memoryCache.setObject(CacheEntry(image), forKey: key as NSString, cost: Self.decodedImageCost(image))
        }
        if image == nil && !cancelled {
            let now = Date()
            failedUntil = failedUntil.filter { $0.value > now }
            if failedUntil.count >= 256, let oldest = failedUntil.min(by: { $0.value < $1.value })?.key {
                failedUntil.removeValue(forKey: oldest)
            }
            failedUntil[key] = now.addingTimeInterval(failureCooldown)
        }
        for continuation in pending.waiters.values {
            continuation.resume(returning: image)
        }
    }

    private func downloadImage(for url: URL, maxPixelSize: CGFloat) async -> PlatformImage? {
        let start = ProcessInfo.processInfo.systemUptime
        let trace = UUID().uuidString.prefix(8)
        let resource = LoadingDiagnostics.resource(url)
        var outcome = "failed"
        LoadingDiagnostics.log("image start id=\(trace) resource=\(resource) pixels=\(Int(maxPixelSize))")
        defer { LoadingDiagnostics.log("image end id=\(trace) outcome=\(outcome) totalMs=\(LoadingDiagnostics.milliseconds(since: start))") }
        do {
            let (temporaryURL, response) = try await download(url)
            LoadingDiagnostics.log("image response id=\(trace) downloadMs=\(LoadingDiagnostics.milliseconds(since: start)) status=\((response as? HTTPURLResponse)?.statusCode ?? 0) mime=\(response.mimeType ?? "unknown") expectedBytes=\(response.expectedContentLength)")
            defer { try? FileManager.default.removeItem(at: temporaryURL) }
            try Task.checkCancellation()
            guard let http = response as? HTTPURLResponse, (200 ..< 300).contains(http.statusCode) else {
                outcome = "http-rejected"
                return nil
            }
            guard response.expectedContentLength <= 0
                    || response.expectedContentLength <= Self.maximumSourceImageBytes
            else {
                return nil
            }
            let resourceValues = try temporaryURL.resourceValues(forKeys: [.fileSizeKey])
            guard let fileSize = resourceValues.fileSize,
                  Int64(fileSize) <= Self.maximumSourceImageBytes
            else {
                return nil
            }
            let data = try Data(contentsOf: temporaryURL, options: .mappedIfSafe)
            let decodeStart = ProcessInfo.processInfo.systemUptime
            let decoded = Self.decodeImage(data: data, maxPixelSize: maxPixelSize)
            LoadingDiagnostics.log("image decode id=\(trace) bytes=\(data.count) decodeMs=\(LoadingDiagnostics.milliseconds(since: decodeStart)) success=\(decoded != nil)")
            guard let decoded else {
                outcome = "decode-failed"
                return nil
            }
            try Task.checkCancellation()
            outcome = "success"
            return decoded
        } catch {
            outcome = Task.isCancelled ? "cancelled" : LoadingDiagnostics.failure(error)
            return nil
        }
    }

    func prefetch(urls: [URL], maxPixelSize: CGFloat, concurrency: Int = 6) async {
        let unique = Array(Set(urls))
        guard !unique.isEmpty else { return }

        await withTaskGroup(of: Void.self) { group in
            var iterator = unique.makeIterator()
            let initial = min(concurrency, unique.count)
            for _ in 0 ..< initial {
                guard let url = iterator.next() else { break }
                group.addTask {
                    _ = await self.image(for: url, maxPixelSize: maxPixelSize)
                }
            }
            for await _ in group {
                guard let url = iterator.next() else { continue }
                group.addTask {
                    _ = await self.image(for: url, maxPixelSize: maxPixelSize)
                }
            }
        }
    }

    private func cacheKey(url: URL, maxPixelSize: CGFloat) -> String {
        "\(url.absoluteString)|\(Int(maxPixelSize))"
    }

    private static func decodeImage(data: Data, maxPixelSize: CGFloat) -> PlatformImage? {
        guard maxPixelSize > 0 else { return PlatformImage(data: data) }

        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize,
            kCGImageSourceCreateThumbnailWithTransform: true,
        ]
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              let cgImage = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary)
        else {
            let fallback = PlatformImage(data: data)
            LoadingDiagnostics.log("image thumbnail-fallback pixels=\(Int(maxPixelSize)) success=\(fallback != nil)")
            return fallback
        }
#if canImport(UIKit)
        return PlatformImage(cgImage: cgImage)
#elseif canImport(AppKit)
        return PlatformImage(
            cgImage: cgImage,
            size: NSSize(width: cgImage.width, height: cgImage.height)
        )
#endif
    }

    private static func decodedImageCost(_ image: PlatformImage) -> Int {
#if canImport(UIKit)
        guard let cgImage = image.cgImage else { return 0 }
#elseif canImport(AppKit)
        guard let cgImage = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else { return 0 }
#endif
        return cgImage.bytesPerRow * cgImage.height
    }
}
