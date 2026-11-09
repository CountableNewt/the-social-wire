struct GatewayOAuthMetadataDocument: Encodable, Sendable {
  let client_id: String
  let application_type: String
  let redirect_uris: [String]
  let scope: String
  let client_name: String
  let client_uri: String
  let grant_types = ["authorization_code", "refresh_token"]
  let response_types = ["code"]
  let token_endpoint_auth_method = "none"
  let dpop_bound_access_tokens = true
}
