import Foundation
import Hummingbird
import HTTPTypes
import Logging
import SkejKit

public struct ErrorBody: Codable, Sendable {
    public let error: String
    public let message: String
}

public struct APIError: Error, Sendable {
    public let status: HTTPResponse.Status
    public let code: String
    public let message: String

    public init(status: HTTPResponse.Status, code: String, message: String) {
        self.status = status
        self.code = code
        self.message = message
    }
}

public func jsonResponse<T: Encodable>(
    _ body: T,
    status: HTTPResponse.Status = .ok
) throws -> Response {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.sortedKeys]
    let data = try encoder.encode(body)
    var buffer = ByteBuffer()
    buffer.writeBytes(data)
    var headers = HTTPFields()
    headers[.contentType] = "application/json; charset=utf-8"
    return Response(status: status, headers: headers, body: .init(byteBuffer: buffer))
}

public func decodeJSONBody<T: Decodable>(_ request: Request, as type: T.Type) async throws -> T {
    do {
        let buffer = try await request.body.collect(upTo: 1_048_576)
        return try JSONDecoder().decode(T.self, from: Data(buffer: buffer))
    } catch let error as APIError {
        throw error
    } catch {
        throw APIError(status: .badRequest, code: "invalid_json", message: "Invalid JSON body")
    }
}

public func errorResponse(_ error: Error) -> Response {
    // Reads fall back to cached records, so an auth failure reaching here came
    // from a write. The account's Skej session is still valid — only its PDS
    // credentials are — so this must not be a 401, which the web client treats
    // as "signed out" and would bounce the user to the sign-in screen.
    if PDSClientError.isAuthFailure(error) {
        return errorResponse(APIError(
            status: .conflict,
            code: "account_needs_reauth",
            message: "Reconnect this account to keep posting from it."
        ))
    }
    if let apiError = error as? APIError {
        return (try? jsonResponse(
            ErrorBody(error: apiError.code, message: apiError.message),
            status: apiError.status
        )) ?? Response(status: apiError.status)
    }
    if let httpError = error as? HTTPError {
        let code = httpError.status.reasonPhrase.lowercased().replacingOccurrences(of: " ", with: "_")
        return (try? jsonResponse(
            ErrorBody(error: code, message: httpError.status.reasonPhrase),
            status: httpError.status
        )) ?? Response(status: httpError.status)
    }
    return (try? jsonResponse(
        ErrorBody(error: "internal_error", message: "Internal server error"),
        status: .internalServerError
    )) ?? Response(status: .internalServerError)
}

public struct ErrorMiddleware<Context: RequestContext>: RouterMiddleware {
    private let logger = Logger(label: "skej.errors")

    public init() {}

    public func handle(
        _ request: Request,
        context: Context,
        next: (Request, Context) async throws -> Response
    ) async throws -> Response {
        do {
            return try await next(request, context)
        } catch {
            // These all map to deliberate responses; anything else became an
            // opaque 500, so log it or the cause is unrecoverable after the fact.
            if !(error is APIError), !(error is HTTPError), !PDSClientError.isAuthFailure(error) {
                logger.error("unhandled request error", metadata: [
                    "method": "\(request.method)",
                    "path": "\(request.uri.path)",
                    "error_type": "\(String(reflecting: type(of: error)))",
                ])
            }
            return errorResponse(error)
        }
    }
}
