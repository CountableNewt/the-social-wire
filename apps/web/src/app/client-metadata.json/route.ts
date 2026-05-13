export async function GET() {
  return Response.json(
    {
      client_id: "https://thesocialwire.com/client-metadata.json",
      application_type: "web",
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      redirect_uris: ["https://thesocialwire.com/callback"],
      scope: "atproto",
      token_endpoint_auth_method: "none",
      dpop_bound_access_tokens: true,
      client_name: "The Social Wire",
      client_uri: "https://thesocialwire.com",
    },
    {
      headers: {
        "Access-Control-Allow-Origin": "*",
        "Content-Type": "application/json",
      },
    }
  );
}
