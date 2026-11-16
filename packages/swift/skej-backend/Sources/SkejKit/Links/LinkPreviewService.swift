import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
#if canImport(Darwin)
import Darwin
#elseif canImport(Glibc)
import Glibc
#endif
import Logging
import SwiftSoup

public protocol LinkPreviewHTTPClient: Sendable {
    func fetch(url: URL, accept: String, maxBytes: Int) async throws -> LinkPreviewHTTPResponse
}

public protocol LinkPreviewHydrating: Sendable {
    func hydrate(did: String, url: String) async throws -> ExternalEmbed
}

public struct LinkPreviewHTTPResponse: Sendable {
    public let body: Data
    public let mimeType: String
    public let finalURL: URL

    public init(body: Data, mimeType: String, finalURL: URL) {
        self.body = body
        self.mimeType = mimeType
        self.finalURL = finalURL
    }
}

public enum LinkPreviewError: Error, Equatable {
    case invalidURL
    case unsafeURL
    case tooManyRedirects
    case responseTooLarge
    case unsupportedContentType
    case badStatus(Int)
    case invalidResponse
}

public struct LinkPreviewService: LinkPreviewHydrating, Sendable {
    private let pdsClient: any PDSClient
    private let http: any LinkPreviewHTTPClient
    private let logger: Logger

    public init(
        pdsClient: any PDSClient,
        http: any LinkPreviewHTTPClient = SafeLinkPreviewHTTPClient(),
        logger: Logger = Logger(label: "skej.link-preview")
    ) {
        self.pdsClient = pdsClient
        self.http = http
        self.logger = logger
    }

    public func hydrate(did: String, url rawURL: String) async throws -> ExternalEmbed {
        let trimmedURL = rawURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let requestedURL = URL(string: trimmedURL),
              requestedURL.scheme?.lowercased() == "http" ||
                requestedURL.scheme?.lowercased() == "https",
              requestedURL.host != nil,
              requestedURL.user == nil,
              requestedURL.password == nil
        else {
            throw LinkPreviewError.invalidURL
        }

        let page = try await http.fetch(
            url: requestedURL,
            accept: "text/html,application/xhtml+xml",
            maxBytes: 2_000_000
        )
        guard page.mimeType.lowercased().hasPrefix("text/html") ||
                page.mimeType.lowercased().hasPrefix("application/xhtml+xml")
        else {
            throw LinkPreviewError.unsupportedContentType
        }
        guard let html = decodeHTML(page.body) else {
            throw LinkPreviewError.invalidResponse
        }

        let document = try SwiftSoup.parse(html, page.finalURL.absoluteString)
        let title = firstContent(
            in: document,
            selectors: [
                "meta[property=og:title]",
                "meta[name=twitter:title]",
            ]
        ) ?? (try? document.title()).flatMap(nonEmpty)
            ?? requestedURL.host
            ?? trimmedURL
        let description = firstContent(
            in: document,
            selectors: [
                "meta[property=og:description]",
                "meta[name=twitter:description]",
                "meta[name=description]",
            ]
        ) ?? ""
        let imageValue = firstContent(
            in: document,
            selectors: [
                "meta[property=og:image]",
                "meta[property=og:image:url]",
                "meta[name=twitter:image]",
                "meta[name=twitter:image:src]",
            ]
        )

        var thumb: ATProtoBlobReference?
        if let imageValue,
           let imageURL = URL(string: imageValue, relativeTo: page.finalURL)?.absoluteURL
        {
            do {
                let image = try await http.fetch(
                    url: imageURL,
                    accept: "image/*",
                    maxBytes: 1_000_000
                )
                guard image.mimeType.lowercased().hasPrefix("image/") else {
                    throw LinkPreviewError.unsupportedContentType
                }
                thumb = try await pdsClient.uploadBlob(
                    did: did,
                    data: image.body,
                    mimeType: image.mimeType
                )
            } catch {
                // The link remains publishable when a remote thumbnail cannot be used.
                logger.warning(
                    "link preview thumbnail omitted",
                    metadata: [
                        "host": "\(imageURL.host ?? "unknown")",
                        "category": "\(String(describing: type(of: error)))",
                    ]
                )
                thumb = nil
            }
        }

        return ExternalEmbed(
            external: ExternalEmbedContent(
                uri: trimmedURL,
                title: title,
                description: description,
                thumb: thumb
            )
        )
    }

    private func firstContent(in document: Document, selectors: [String]) -> String? {
        for selector in selectors {
            if let element = try? document.select(selector).first(),
               let content = try? element.attr("content"),
               let content = nonEmpty(content)
            {
                return content
            }
        }
        return nil
    }

    private func nonEmpty(_ value: String) -> String? {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    private func decodeHTML(_ data: Data) -> String? {
        String(data: data, encoding: .utf8)
            ?? String(data: data, encoding: .isoLatin1)
    }
}

public struct SafeLinkPreviewHTTPClient: LinkPreviewHTTPClient {
    private let timeout: TimeInterval
    private let maxRedirects: Int

    public init(timeout: TimeInterval = 10, maxRedirects: Int = 5) {
        self.timeout = timeout
        self.maxRedirects = maxRedirects
    }

    public func fetch(url: URL, accept: String, maxBytes: Int) async throws -> LinkPreviewHTTPResponse {
        var currentURL = url
        for redirectCount in 0...maxRedirects {
            try SafeURLValidator.validate(currentURL)
            var request = URLRequest(url: currentURL, timeoutInterval: timeout)
            request.setValue(accept, forHTTPHeaderField: "Accept")
            request.setValue("Skej Link Preview/1.0", forHTTPHeaderField: "User-Agent")
            let configuration = URLSessionConfiguration.ephemeral
            configuration.timeoutIntervalForRequest = timeout
            configuration.timeoutIntervalForResource = timeout
            let (body, response) = try await CappedURLSessionRequest(
                configuration: configuration,
                maxBytes: maxBytes
            ).perform(request)
            guard let httpResponse = response as? HTTPURLResponse else {
                throw LinkPreviewError.invalidResponse
            }

            if (300..<400).contains(httpResponse.statusCode) {
                guard redirectCount < maxRedirects else {
                    throw LinkPreviewError.tooManyRedirects
                }
                guard let location = httpResponse.value(forHTTPHeaderField: "Location"),
                      let nextURL = URL(string: location, relativeTo: currentURL)?.absoluteURL
                else {
                    throw LinkPreviewError.invalidResponse
                }
                currentURL = nextURL
                continue
            }
            guard (200..<300).contains(httpResponse.statusCode) else {
                throw LinkPreviewError.badStatus(httpResponse.statusCode)
            }
            if let expectedLength = httpResponse.value(forHTTPHeaderField: "Content-Length"),
               let expectedBytes = Int(expectedLength),
               expectedBytes > maxBytes
            {
                throw LinkPreviewError.responseTooLarge
            }
            guard body.count <= maxBytes else {
                throw LinkPreviewError.responseTooLarge
            }
            let mimeType = httpResponse.value(forHTTPHeaderField: "Content-Type")?
                .split(separator: ";", maxSplits: 1)
                .first
                .map(String.init)
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                ?? httpResponse.mimeType
                ?? "application/octet-stream"
            return LinkPreviewHTTPResponse(
                body: body,
                mimeType: mimeType,
                finalURL: httpResponse.url ?? currentURL
            )
        }
        throw LinkPreviewError.tooManyRedirects
    }
}

private final class CappedURLSessionRequest: NSObject, URLSessionDataDelegate, @unchecked Sendable {
    private let configuration: URLSessionConfiguration
    private let maxBytes: Int
    private var continuation: CheckedContinuation<(Data, URLResponse), Error>?
    private var response: URLResponse?
    private var body = Data()
    private var session: URLSession?

    init(configuration: URLSessionConfiguration, maxBytes: Int) {
        self.configuration = configuration
        self.maxBytes = maxBytes
    }

    func perform(_ request: URLRequest) async throws -> (Data, URLResponse) {
        try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
            let session = URLSession(
                configuration: configuration,
                delegate: self,
                delegateQueue: nil
            )
            self.session = session
            session.dataTask(with: request).resume()
        }
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest,
        completionHandler: @escaping (URLRequest?) -> Void
    ) {
        completionHandler(nil)
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive response: URLResponse,
        completionHandler: @escaping (URLSession.ResponseDisposition) -> Void
    ) {
        if response.expectedContentLength > Int64(maxBytes) {
            completionHandler(.cancel)
            finish(throwing: LinkPreviewError.responseTooLarge)
            return
        }
        self.response = response
        completionHandler(.allow)
    }

    func urlSession(
        _ session: URLSession,
        dataTask: URLSessionDataTask,
        didReceive data: Data
    ) {
        guard continuation != nil else { return }
        guard body.count + data.count <= maxBytes else {
            dataTask.cancel()
            finish(throwing: LinkPreviewError.responseTooLarge)
            return
        }
        body.append(data)
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        didCompleteWithError error: Error?
    ) {
        guard continuation != nil else { return }
        if let error {
            finish(throwing: error)
        } else if let response {
            finish(returning: (body, response))
        } else {
            finish(throwing: LinkPreviewError.invalidResponse)
        }
    }

    private func finish(returning value: (Data, URLResponse)) {
        guard let continuation else { return }
        self.continuation = nil
        session?.finishTasksAndInvalidate()
        session = nil
        continuation.resume(returning: value)
    }

    private func finish(throwing error: Error) {
        guard let continuation else { return }
        self.continuation = nil
        session?.invalidateAndCancel()
        session = nil
        continuation.resume(throwing: error)
    }
}

public enum SafeURLValidator {
    public static func validate(_ url: URL) throws {
        guard let scheme = url.scheme?.lowercased(),
              scheme == "http" || scheme == "https",
              let host = url.host?.lowercased(),
              !host.isEmpty,
              url.user == nil,
              url.password == nil,
              host != "localhost",
              !host.hasSuffix(".localhost"),
              !host.hasSuffix(".local"),
              !host.hasSuffix(".internal")
        else {
            throw LinkPreviewError.unsafeURL
        }

        let addresses = try resolvedAddresses(host: host)
        guard !addresses.isEmpty, addresses.allSatisfy(isPublicAddress) else {
            throw LinkPreviewError.unsafeURL
        }
    }

    private static func resolvedAddresses(host: String) throws -> [String] {
        var hints = addrinfo()
        hints.ai_family = AF_UNSPEC
        hints.ai_socktype = socketStream
        var result: UnsafeMutablePointer<addrinfo>?
        guard getaddrinfo(host, nil, &hints, &result) == 0, let first = result else {
            throw LinkPreviewError.unsafeURL
        }
        defer { freeaddrinfo(first) }

        var addresses: [String] = []
        var current: UnsafeMutablePointer<addrinfo>? = first
        while let info = current {
            var buffer = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            if getnameinfo(
                info.pointee.ai_addr,
                info.pointee.ai_addrlen,
                &buffer,
                socklen_t(buffer.count),
                nil,
                0,
                NI_NUMERICHOST
            ) == 0 {
                let end = buffer.firstIndex(of: 0) ?? buffer.endIndex
                addresses.append(String(
                    decoding: buffer[..<end].map(UInt8.init(bitPattern:)),
                    as: UTF8.self
                ))
            }
            current = info.pointee.ai_next
        }
        return addresses
    }

    private static func isPublicAddress(_ value: String) -> Bool {
        var ipv4 = in_addr()
        if inet_pton(AF_INET, value, &ipv4) == 1 {
            let address = UInt32(bigEndian: ipv4.s_addr)
            return isPublicIPv4([
                UInt8((address >> 24) & 0xff),
                UInt8((address >> 16) & 0xff),
                UInt8((address >> 8) & 0xff),
                UInt8(address & 0xff),
            ])
        }

        var ipv6 = in6_addr()
        if inet_pton(AF_INET6, value, &ipv6) == 1 {
            let bytes = withUnsafeBytes(of: &ipv6) { Array($0) }
            if bytes.allSatisfy({ $0 == 0 }) { return false }
            if bytes.dropLast().allSatisfy({ $0 == 0 }) && bytes.last == 1 { return false }
            if bytes[0] == 0xff || (bytes[0] & 0xfe) == 0xfc { return false }
            if bytes[0] == 0xfe && (bytes[1] & 0xc0) == 0x80 { return false }
            if bytes[0] == 0xfe && (bytes[1] & 0xc0) == 0xc0 { return false }
            if bytes[0...3] == [0x20, 0x01, 0x0d, 0xb8] { return false }
            if bytes[0..<10].allSatisfy({ $0 == 0 }) &&
                bytes[10] == 0xff &&
                bytes[11] == 0xff
            {
                return isPublicIPv4(Array(bytes[12..<16]))
            }
            return true
        }
        return false
    }

    private static func isPublicIPv4(_ bytes: [UInt8]) -> Bool {
        guard bytes.count == 4 else { return false }
        let first = bytes[0]
        let second = bytes[1]
        let third = bytes[2]
        if first == 0 || first == 10 || first == 127 || first >= 224 { return false }
        if first == 100 && (64...127).contains(second) { return false }
        if first == 169 && second == 254 { return false }
        if first == 172 && (16...31).contains(second) { return false }
        if first == 192 && second == 0 && (third == 0 || third == 2) { return false }
        if first == 192 && second == 168 { return false }
        if first == 192 && second == 88 && third == 99 { return false }
        if first == 198 && (second == 18 || second == 19) { return false }
        if first == 198 && second == 51 && third == 100 { return false }
        if first == 203 && second == 0 && third == 113 { return false }
        return true
    }

    private static var socketStream: Int32 {
        #if canImport(Glibc)
        Int32(SOCK_STREAM.rawValue)
        #else
        SOCK_STREAM
        #endif
    }
}
