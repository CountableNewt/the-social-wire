@preconcurrency import Crypto
import Foundation
import SkejKit
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

public protocol OAuthClient: Sendable {
    func start(handle: String, state: String, pkceVerifier: String, nonce: String) async throws -> OAuthStartResult
    func complete(state: OAuthStateRecord, code: String) async throws -> OAuthCompletion
}

public struct OAuthStartResult: Sendable, Equatable {
    public let redirectURL: String
    public let authServer: String
    public let tokenEndpoint: String
    public let pdsEndpoint: String
    public let dpopKeyJSON: String

    public init(
        redirectURL: String,
        authServer: String,
        tokenEndpoint: String,
        pdsEndpoint: String,
        dpopKeyJSON: String
    ) {
        self.redirectURL = redirectURL
        self.authServer = authServer
        self.tokenEndpoint = tokenEndpoint
        self.pdsEndpoint = pdsEndpoint
        self.dpopKeyJSON = dpopKeyJSON
    }
}

public struct OAuthCompletion: Sendable, Equatable {
    public let viewer: Viewer
    public let session: OAuthSessionRecord

    public init(viewer: Viewer, session: OAuthSessionRecord) {
        self.viewer = viewer
        self.session = session
    }
}

struct ATProtoOAuthClient: OAuthClient {
    private let config: AppConfig
    private let http: HTTPClient
    private let scope = AppConfig.scopes.joined(separator: " ")

    public init(config: AppConfig, http: HTTPClient = URLSessionHTTPClient()) {
        self.config = config
        self.http = http
    }

    public func start(handle: String, state: String, pkceVerifier: String, nonce: String) async throws -> OAuthStartResult {
        let identity = try await resolveIdentity(handle)
        let resource = try await fetchProtectedResourceMetadata(pdsEndpoint: identity.pdsEndpoint)
        let authServer = resource.authorizationServers.first ?? identity.pdsEndpoint
        let authorization = try await fetchAuthorizationServerMetadata(authServer: authServer)
        let dpopKey = DPoPKey()
        let requestBody = formEncoded([
            "client_id": config.clientID,
            "redirect_uri": config.redirectURI,
            "response_type": "code",
            "scope": scope,
            "state": state,
            "code_challenge": pkceChallenge(pkceVerifier),
            "code_challenge_method": "S256",
            "login_hint": handle,
        ])
        let response = try await postWithDPoPNonceRetry(
            url: authorization.pushedAuthorizationRequestEndpoint,
            dpopKey: dpopKey,
            body: requestBody,
            accessToken: nil
        )
        let par = try JSONDecoder().decode(PARResponse.self, from: response.body)
        guard var redirect = URLComponents(string: authorization.authorizationEndpoint) else {
            throw OAuthClientError.authorizationServerUnsupported("Invalid authorization endpoint")
        }
        redirect.queryItems = (redirect.queryItems ?? []) + [
            URLQueryItem(name: "client_id", value: config.clientID),
            URLQueryItem(name: "request_uri", value: par.requestURI),
        ]
        guard let redirectURL = redirect.string else {
            throw OAuthClientError.authorizationServerUnsupported("Invalid authorization endpoint")
        }
        return OAuthStartResult(
            redirectURL: redirectURL,
            authServer: authServer,
            tokenEndpoint: authorization.tokenEndpoint,
            pdsEndpoint: identity.pdsEndpoint,
            dpopKeyJSON: try dpopKey.exportJSON()
        )
    }

    public func complete(state: OAuthStateRecord, code: String) async throws -> OAuthCompletion {
        guard let tokenEndpoint = state.tokenEndpoint,
              let pdsEndpoint = state.pdsEndpoint,
              let dpopKeyJSON = state.dpopKeyJSON
        else {
            throw OAuthClientError.invalidState("OAuth state is missing authorization metadata")
        }

        let dpopKey = try DPoPKey(json: dpopKeyJSON)
        let tokenResponse = try await tokenRequest(
            endpoint: tokenEndpoint,
            dpopKey: dpopKey,
            form: [
                "grant_type": "authorization_code",
                "code": code,
                "redirect_uri": config.redirectURI,
                "client_id": config.clientID,
                "code_verifier": state.pkceVerifier,
            ]
        )
        guard tokenResponse.sub == state.initiatorDid,
              tokenResponse.tokenType.lowercased() == "dpop",
              tokenResponse.refreshToken?.isEmpty == false,
              Set(tokenResponse.scope.split(separator: " ").map(String.init)).isSuperset(of: AppConfig.scopes)
        else {
            throw OAuthClientError.invalidToken("Scheduling authorization did not grant the requested account and permissions")
        }

        let payload = ATProtoTokenPayload(
            accessToken: tokenResponse.accessToken,
            refreshToken: tokenResponse.refreshToken,
            tokenType: tokenResponse.tokenType,
            expiresIn: tokenResponse.expiresIn,
            scope: tokenResponse.scope,
            sub: tokenResponse.sub,
            pdsEndpoint: pdsEndpoint,
            tokenEndpoint: tokenEndpoint
        )
        let tokenJSON = try JSONEncoder().encodeString(payload)
        let viewer = Viewer(
            did: tokenResponse.sub,
            handle: state.handle,
            displayName: state.handle,
            avatar: nil
        )
        return OAuthCompletion(
            viewer: viewer,
            session: OAuthSessionRecord(
                did: tokenResponse.sub,
                handle: viewer.handle,
                tokenJSON: tokenJSON,
                dpopKeyJSON: dpopKeyJSON
            )
        )
    }

    private func tokenRequest(endpoint: String, dpopKey: DPoPKey, form: [String: String]) async throws -> TokenResponse {
        let response = try await postWithDPoPNonceRetry(
            url: endpoint,
            dpopKey: dpopKey,
            body: formEncoded(form),
            accessToken: nil
        )
        return try JSONDecoder().decode(TokenResponse.self, from: response.body)
    }

    private func resolveIdentity(_ handle: String) async throws -> ResolvedIdentity {
        let didDocument = try await fetchDIDDocument(did: handle)
        guard didDocument.id == handle else {
            throw OAuthClientError.identityResolutionFailed("DID document does not match the scheduling account")
        }
        return ResolvedIdentity(did: handle, pdsEndpoint: try didDocument.pdsEndpoint())
    }

    private func fetchDIDDocument(did: String) async throws -> DIDDocument {
        let url: String
        if did.starts(with: "did:plc:") {
            url = "\(config.network.plcOrigin)/\(urlEncode(did))"
        } else if did.starts(with: "did:web:") {
            let parts = did.dropFirst(8).split(separator: ":", omittingEmptySubsequences: false)
            guard let authority = parts.first?.removingPercentEncoding,
                  let origin = SkejATProtoNetwork.origin("https://" + authority),
                  parts.dropFirst().allSatisfy({ !$0.isEmpty && $0.allSatisfy({ $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_" || $0 == ".") }) && $0 != "." && $0 != ".." }) else {
                throw OAuthClientError.identityResolutionFailed("DID is outside the configured ATProto network")
            }
            url = origin + (parts.count == 1 ? "/.well-known/did.json" : "/" + parts.dropFirst().joined(separator: "/") + "/did.json")
        } else {
            throw OAuthClientError.identityResolutionFailed("Unsupported DID method")
        }
        let response = try await http.data(HTTPRequest(url: url))
        return try JSONDecoder().decode(DIDDocument.self, from: response.body)
    }

    private func fetchProtectedResourceMetadata(pdsEndpoint: String) async throws -> ProtectedResourceMetadata {
        guard config.network.allowsPDS(pdsEndpoint) else { throw HTTPClientError.invalidURL("PDS is outside the configured ATProto network") }
        let response = try await http.data(HTTPRequest(url: "\(pdsEndpoint)/.well-known/oauth-protected-resource"))
        return try JSONDecoder().decode(ProtectedResourceMetadata.self, from: response.body)
    }

    private func fetchAuthorizationServerMetadata(authServer: String) async throws -> AuthorizationServerMetadata {
        guard config.network.allowsPDS(authServer) else { throw HTTPClientError.invalidURL("OAuth server is outside the configured ATProto network") }
        let response = try await http.data(HTTPRequest(url: "\(authServer)/.well-known/oauth-authorization-server"))
        let metadata = try JSONDecoder().decode(AuthorizationServerMetadata.self, from: response.body)
        guard config.network.allowsOAuthEndpoint(metadata.authorizationEndpoint),
              config.network.allowsOAuthEndpoint(metadata.tokenEndpoint),
              config.network.allowsOAuthEndpoint(metadata.pushedAuthorizationRequestEndpoint) else {
            throw OAuthClientError.authorizationServerUnsupported("OAuth endpoints are outside the configured ATProto network")
        }
        guard metadata.issuer == authServer, metadata.scopesSupported.contains("atproto") else {
            throw OAuthClientError.authorizationServerUnsupported("Authorization server does not advertise atproto")
        }
        return metadata
    }

    private func postWithDPoPNonceRetry(
        url: String,
        dpopKey: DPoPKey,
        body: Data,
        accessToken: String?
    ) async throws -> HTTPResponseData {
        do {
            return try await postWithDPoP(url: url, dpopKey: dpopKey, body: body, accessToken: accessToken, nonce: nil)
        } catch {
            guard let nonce = dpopNonce(from: error) else { throw error }
            return try await postWithDPoP(url: url, dpopKey: dpopKey, body: body, accessToken: accessToken, nonce: nonce)
        }
    }

    private func postWithDPoP(
        url: String,
        dpopKey: DPoPKey,
        body: Data,
        accessToken: String?,
        nonce: String?
    ) async throws -> HTTPResponseData {
        let proof = try dpopKey.proof(httpMethod: "POST", url: url, accessToken: accessToken, nonce: nonce)
        var headers = [
            "Content-Type": "application/x-www-form-urlencoded",
            "DPoP": proof,
        ]
        if let accessToken {
            headers["Authorization"] = "DPoP \(accessToken)"
        }
        return try await http.data(HTTPRequest(url: url, method: "POST", headers: headers, body: body))
    }


}

public enum OAuthClientError: Error, Equatable {
    case identityResolutionFailed(String)
    case authorizationServerUnsupported(String)
    case invalidState(String)
    case invalidToken(String)
}

private struct ResolvedIdentity {
    let did: String
    let pdsEndpoint: String
}

private struct ResolveHandleResponse: Codable {
    let did: String
}

private struct DIDDocument: Codable {
    let id: String
    let service: [DIDService]

    func pdsEndpoint() throws -> String {
        guard let service = service.first(where: { $0.id == "#atproto_pds" || $0.type == "AtprotoPersonalDataServer" }) else {
            throw OAuthClientError.identityResolutionFailed("DID document does not declare an atproto PDS")
        }
        return service.serviceEndpoint.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }
}

private struct DIDService: Codable {
    let id: String
    let type: String
    let serviceEndpoint: String
}

private struct ProtectedResourceMetadata: Codable {
    let authorizationServers: [String]

    enum CodingKeys: String, CodingKey {
        case authorizationServers = "authorization_servers"
    }
}

private struct AuthorizationServerMetadata: Codable {
    let issuer: String
    let authorizationEndpoint: String
    let tokenEndpoint: String
    let pushedAuthorizationRequestEndpoint: String
    let scopesSupported: [String]

    enum CodingKeys: String, CodingKey {
        case authorizationEndpoint = "authorization_endpoint"
        case tokenEndpoint = "token_endpoint"
        case pushedAuthorizationRequestEndpoint = "pushed_authorization_request_endpoint"
        case issuer
        case scopesSupported = "scopes_supported"
    }
}

private struct PARResponse: Codable {
    let requestURI: String

    enum CodingKeys: String, CodingKey {
        case requestURI = "request_uri"
    }
}

private struct TokenResponse: Codable {
    let accessToken: String
    let refreshToken: String?
    let tokenType: String
    let expiresIn: Int?
    let scope: String
    let sub: String

    enum CodingKeys: String, CodingKey {
        case accessToken = "access_token"
        case refreshToken = "refresh_token"
        case tokenType = "token_type"
        case expiresIn = "expires_in"
        case scope
        case sub
    }
}

private struct ATProtoProfile: Codable {
    let did: String
    let handle: String?
    let displayName: String?
    let avatar: String?
}
