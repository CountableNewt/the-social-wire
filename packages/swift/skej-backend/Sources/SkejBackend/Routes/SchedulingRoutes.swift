import Crypto
import Foundation
import HTTPTypes
import Hummingbird
import SkejKit

extension EmbeddedSkejBackend {
    public func registerOAuth<Context: RequestContext>(
        on router: Router<Context>,
        canConnect: @escaping @Sendable (String) -> Bool = { _ in true }
    ) {
        let oauth = router.group().add(middleware: ErrorMiddleware<Context>())
        oauth.get("/oauth/scheduling/client-metadata.json") { _, _ in
            try jsonResponse(OAuthMetadata.metadata(config: config))
        }
        oauth.get("/oauth/scheduling/start") { request, _ in
            try validateQuery(request, allowed: ["accountDid"])
            guard let did = request.uri.queryParameters.get("accountDid"),
                  did.utf8.count <= 512, did.range(of: #"^did:(plc:[a-z2-7]{24}|web:[A-Za-z0-9.%:_-]+)$"#, options: .regularExpression) != nil
            else { throw APIError(status: .badRequest, code: "InvalidActor", message: "An account DID is required") }

            guard canConnect(did) else {
                throw APIError(status: .forbidden, code: "ProRequired", message: "A Pro subscription is required")
            }
            let state = randomToken()
            let pkceVerifier = randomToken()
            let nonce = randomToken()
            let start = try await oauthClient.start(handle: did, state: state, pkceVerifier: pkceVerifier, nonce: nonce)
            try await store.createOAuthState(
                state: state, handle: did, pkceVerifier: pkceVerifier, nonce: nonce,
                authServer: start.authServer, tokenEndpoint: start.tokenEndpoint,
                pdsEndpoint: start.pdsEndpoint, dpopKeyJSON: start.dpopKeyJSON,
                initiatorDid: did, expiresAt: Timestamp.iso8601(Date().addingTimeInterval(600))
            )
            var headers = HTTPFields()
            headers[.location] = start.redirectURL
            headers[.cacheControl] = "no-store"
            headers[HTTPField.Name("Referrer-Policy")!] = "no-referrer"
            return Response(status: .found, headers: headers)
        }
        oauth.get("/oauth/scheduling/callback") { request, _ in
            guard let state = request.uri.queryParameters.get("state"),
                  let code = request.uri.queryParameters.get("code"),
                  state.utf8.count <= 128, code.utf8.count <= 2048,
                  let pending = try await store.consumeOAuthState(state: state, now: Timestamp.iso8601()),
                  let did = pending.initiatorDid,
                  request.uri.queryParameters.get("iss") == pending.authServer
            else { throw APIError(status: .badRequest, code: "InvalidOAuthState", message: "Scheduling authorization has expired") }
            guard canConnect(did) else {
                throw APIError(status: .forbidden, code: "ProRequired", message: "A Pro subscription is required")
            }
            let completion = try await oauthClient.complete(state: pending, code: code)
            guard completion.viewer.did == did, completion.session.did == did
            else { throw APIError(status: .forbidden, code: "ActorMismatch", message: "Authorize the selected account") }

            let now = Timestamp.iso8601()
            try await store.createOAuthSession(completion.session, now: now)
            try await store.upsertManagedAccount(
                ManagedAccount(did: did, handle: completion.viewer.handle, displayName: completion.viewer.displayName,
                    avatar: completion.viewer.avatar, pdsEndpoint: pending.pdsEndpoint, status: .active, isDefault: true),
                now: now
            )
            try await reconcileLegacyQueue(did: did)
            try await repairEmbeddedQueue(did: did)
            return SchedulingConnectedPage.response()
        }
    }

    public func registerScheduling<Context: RequestContext>(
        on group: RouterGroup<Context>,
        actorDID: @escaping @Sendable (Context) -> String?
    ) {
        let routes = group.group().add(middleware: ErrorMiddleware<Context>())
        routes.get("/xrpc/at.skej.actor.getSession") { request, context in
            try validateQuery(request, allowed: [])
            let did = try scheduleActor(actorDID(context), requested: nil)
            let account = try await connectedAccount(did: did)
            try await reconcileLegacyQueue(did: did)
            try await repairEmbeddedQueue(did: did)
            return try scheduleJSON(Viewer(did: did, handle: account.handle, displayName: account.displayName, avatar: account.avatar))
        }
        routes.get("/xrpc/at.skej.schedule.list") { request, context in
            try validateQuery(request, allowed: ["accountDid"])
            let did = try scheduleActor(actorDID(context), requested: request.uri.queryParameters.get("accountDid"))
            _ = try await connectedAccount(did: did)
            try await reconcileLegacyQueue(did: did)
            return try scheduleJSON(ListSchedulesResponse(records: try await schedules(did: did)))
        }
        routes.post("/xrpc/at.skej.schedule.create") { request, context in
            let input = try await scheduleInput(request, as: SkejCreateScheduleInput.self)
            let did = try scheduleActor(actorDID(context), requested: input.accountDid)
            _ = try await connectedAccount(did: did)
            try await reconcileLegacyQueue(did: did)
            return try scheduleJSON(try await create(did: did, input: input.record), status: .created)
        }
        routes.post("/xrpc/at.skej.schedule.update") { request, context in
            let input = try await scheduleInput(request, as: SkejUpdateScheduleInput.self)
            let did = try scheduleActor(actorDID(context), requested: input.accountDid)
            _ = try await connectedAccount(did: did)
            return try scheduleJSON(try await edit(did: did, rkey: input.rkey, update: input.record))
        }
        routes.post("/xrpc/at.skej.schedule.cancel") { request, context in
            let input = try await scheduleInput(request, as: SkejScheduleParameters.self)
            let did = try scheduleActor(actorDID(context), requested: input.accountDid)
            _ = try await connectedAccount(did: did)
            return try scheduleJSON(try await edit(did: did, rkey: input.rkey, update: nil))
        }
    }

    private func connectedAccount(did: String) async throws -> ManagedAccount {
        guard let account = try await store.managedAccount(did: did), account.status == .active,
              let stored = try await store.oauthSession(did: did),
              let token = try? JSONDecoder().decode(ATProtoTokenPayload.self, from: Data(stored.tokenJSON.utf8)),
              token.sub == did, !token.accessToken.isEmpty, token.refreshToken?.isEmpty == false,
              token.tokenType.lowercased() == "dpop", config.network.allowsPDS(token.pdsEndpoint),
              config.network.allowsOAuthEndpoint(token.tokenEndpoint), (try? DPoPKey(json: stored.dpopKeyJSON)) != nil
        else {
            throw APIError(status: .conflict, code: "AccountNeedsReauth", message: "Connect this account to scheduling")
        }
        try await pdsClient.verifySchedulingConnection(did: did)
        return account
    }
}

func scheduleActor(_ authenticated: String?, requested: String?) throws -> String {
    guard let did = authenticated, !did.isEmpty else { throw HTTPError(.unauthorized) }
    guard requested == nil || requested == did else {
        throw APIError(status: .forbidden, code: "ActorMismatch", message: "Use the authenticated account for scheduling")
    }
    return did
}

private func validateQuery(_ request: Request, allowed: Set<String>) throws {
    var names = Set<String>()
    for parameter in request.uri.queryParameters {
        let key = String(parameter.key)
        guard allowed.contains(key), names.insert(key).inserted else {
            throw APIError(status: .badRequest, code: "InvalidRequest", message: "Invalid scheduling query parameters")
        }
    }
}

private func scheduleInput<T: Decodable>(_ request: Request, as type: T.Type) async throws -> T {
    try validateQuery(request, allowed: [])
    guard request.headers[.contentType]?.split(separator: ";").first?.trimmingCharacters(in: .whitespaces).lowercased() == "application/json"
    else { throw APIError(status: .unsupportedMediaType, code: "InvalidRequest", message: "Expected a JSON body") }
    return try await decodeJSONBody(request, as: type)
}

private func scheduleJSON<T: Encodable>(_ value: T, status: HTTPResponse.Status = .ok) throws -> Response {
    var response = try jsonResponse(value, status: status)
    response.headers[.cacheControl] = "private, no-store"
    return response
}

private func randomToken() -> String {
    SymmetricKey(size: .bits256).withUnsafeBytes { base64URLEncode(Data($0)) }
}
