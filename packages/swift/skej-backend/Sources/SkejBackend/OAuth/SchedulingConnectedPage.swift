import Hummingbird
import HTTPTypes

/// The completion page contains no identity, session credentials, or cross-origin callback payload.
enum SchedulingConnectedPage {
    static func response() -> Response {
        var headers = HTTPFields()
        headers[.contentType] = "text/html; charset=utf-8"
        headers[.cacheControl] = "no-store"
        headers[HTTPField.Name("Referrer-Policy")!] = "no-referrer"
        headers[HTTPField.Name("Content-Security-Policy")!] = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'"
        return Response(status: .ok, headers: headers, body: .init(byteBuffer: ByteBuffer(string: """
            <!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>Scheduling Connected</title>
            <style>:root{color-scheme:light dark}body{font:1rem/1.6 system-ui,sans-serif;max-width:36rem;margin:12vh auto;padding:1.5rem}h1{font-size:1.5rem}</style></head>
            <body><main><h1>Scheduling Connected</h1><p>Return to Social Wire and select Check Connection.</p><p>You can close this tab.</p></main></body></html>
            """)))
    }
}
