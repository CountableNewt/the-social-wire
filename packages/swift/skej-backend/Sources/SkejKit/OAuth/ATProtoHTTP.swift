@preconcurrency import Crypto
import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public enum SkejCryptoError: Error, Equatable, Sendable {
    case invalidStoredKey
    case encodingFailed
}

public struct ATProtoTokenPayload: Codable, Equatable, Sendable {
    public let accessToken: String
    public let refreshToken: String?
    public let tokenType: String
    public let expiresIn: Int?
    public let scope: String
    public let sub: String
    public let pdsEndpoint: String
    public let tokenEndpoint: String

    public init(accessToken: String, refreshToken: String?, tokenType: String, expiresIn: Int?, scope: String, sub: String, pdsEndpoint: String, tokenEndpoint: String) {
        self.accessToken = accessToken; self.refreshToken = refreshToken; self.tokenType = tokenType
        self.expiresIn = expiresIn; self.scope = scope; self.sub = sub
        self.pdsEndpoint = pdsEndpoint; self.tokenEndpoint = tokenEndpoint
    }
}

public struct HTTPRequest: Sendable {
    public let url: String
    public let method: String
    public let headers: [String: String]
    public let body: Data?

    public init(url: String, method: String = "GET", headers: [String: String] = [:], body: Data? = nil) {
        self.url = url; self.method = method; self.headers = headers; self.body = body
    }
}

public struct HTTPResponseData: Sendable {
    public let body: Data
    public let headers: [String: String]
    public let statusCode: Int

    public init(body: Data, headers: [String: String], statusCode: Int) {
        self.body = body; self.headers = headers; self.statusCode = statusCode
    }
}

public protocol HTTPClient: Sendable {
    func data(_ request: HTTPRequest) async throws -> HTTPResponseData
}

public struct URLSessionHTTPClient: HTTPClient {
    public init() {}

    public func data(_ request: HTTPRequest) async throws -> HTTPResponseData {
        guard let url = URL(string: request.url) else { throw HTTPClientError.invalidURL(request.url) }
        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = request.method
        urlRequest.httpBody = request.body
        for (name, value) in request.headers { urlRequest.setValue(value, forHTTPHeaderField: name) }
        let (body, response) = try await URLSession.shared.data(for: urlRequest)
        guard let http = response as? HTTPURLResponse else { throw HTTPClientError.invalidResponse }
        var headers: [String: String] = [:]
        for (key, value) in http.allHeaderFields { headers[String(describing: key).lowercased()] = String(describing: value) }
        guard (200 ..< 300).contains(http.statusCode) else {
            throw HTTPClientError.badStatus(http.statusCode, String(data: body, encoding: .utf8) ?? "", headers)
        }
        return HTTPResponseData(body: body, headers: headers, statusCode: http.statusCode)
    }
}

public struct DPoPKey: @unchecked Sendable {
    private let privateKey: P256.Signing.PrivateKey

    public init() { self.privateKey = P256.Signing.PrivateKey() }

    public init(json: String) throws {
        let stored = try JSONDecoder().decode(StoredDPoPKey.self, from: Data(json.utf8))
        guard let data = Data(base64Encoded: stored.rawRepresentation) else { throw SkejCryptoError.invalidStoredKey }
        self.privateKey = try P256.Signing.PrivateKey(rawRepresentation: data)
    }

    public func exportJSON() throws -> String {
        let data = try JSONEncoder().encode(StoredDPoPKey(kty: "EC", crv: "P-256", rawRepresentation: privateKey.rawRepresentation.base64EncodedString()))
        guard let string = String(data: data, encoding: .utf8) else { throw SkejCryptoError.encodingFailed }
        return string
    }

    public func proof(httpMethod: String, url: String, accessToken: String? = nil, nonce: String? = nil) throws -> String {
        var payload: [String: JSONValue] = [
            "jti": .string(UUID().uuidString), "htm": .string(httpMethod.uppercased()),
            "htu": .string(try Self.proofTarget(url)), "iat": .number(Double(Int(Date().timeIntervalSince1970))),
        ]
        if let nonce { payload["nonce"] = .string(nonce) }
        if let accessToken { payload["ath"] = .string(base64URLEncode(Data(SHA256.hash(data: Data(accessToken.utf8))))) }
        return try signJWT(header: ["typ": .string("dpop+jwt"), "alg": .string("ES256"), "jwk": .object(publicJWK())], payload: payload)
    }

    private static func proofTarget(_ url: String) throws -> String {
        guard var components = URLComponents(string: url) else { throw HTTPClientError.invalidURL("Invalid DPoP target") }
        components.query = nil
        components.fragment = nil
        guard let target = components.string else { throw HTTPClientError.invalidURL("Invalid DPoP target") }
        return target
    }

    private func signJWT(header: [String: JSONValue], payload: [String: JSONValue]) throws -> String {
        let encoder = JSONEncoder()
        let encodedHeader = base64URLEncode(try encoder.encode(header))
        let encodedPayload = base64URLEncode(try encoder.encode(payload))
        let input = "\(encodedHeader).\(encodedPayload)"
        let signature = try privateKey.signature(for: Data(input.utf8))
        return "\(input).\(base64URLEncode(signature.rawRepresentation))"
    }

    private func publicJWK() -> [String: JSONValue] {
        let representation = privateKey.publicKey.x963Representation
        return ["kty": .string("EC"), "crv": .string("P-256"), "x": .string(base64URLEncode(Data(representation.dropFirst().prefix(32)))), "y": .string(base64URLEncode(Data(representation.dropFirst(33).prefix(32))))]
    }
}

private struct StoredDPoPKey: Codable { let kty: String; let crv: String; let rawRepresentation: String }

public func pkceChallenge(_ verifier: String) -> String { base64URLEncode(Data(SHA256.hash(data: Data(verifier.utf8)))) }
public func formEncoded(_ fields: [String: String]) -> Data { fields.map { "\(urlEncode($0.key))=\(urlEncode($0.value))" }.joined(separator: "&").data(using: .utf8) ?? Data() }
public func urlEncode(_ value: String) -> String { var allowed = CharacterSet.urlQueryAllowed; allowed.remove(charactersIn: ":/?#[]@!$&'()*+,;="); return value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value }
public func base64URLEncode(_ data: Data) -> String { data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "") }
public func dpopNonce(from error: Error) -> String? { guard case HTTPClientError.badStatus(_, _, let headers) = error, let nonce = headers["dpop-nonce"], !nonce.isEmpty else { return nil }; return nonce }
public func localDID(for handle: String) -> String { let digest = SHA256.hash(data: Data(handle.lowercased().utf8)); let suffix = digest.prefix(12).map { String(format: "%02x", $0) }.joined(); return "did:plc:\(suffix)" }
