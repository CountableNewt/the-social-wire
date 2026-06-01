export {
  createSaveUpstreamDpopProofPool,
  createUpstreamDpopProof,
  createUpstreamDpopProofPool,
  pdsXrpcMethodForGatewayRequest,
  primePdsDpopNonce,
  refreshPdsDpopNonce,
} from "latr-packages/gateway-client";

/** PDS XRPC method for Social Wire gateway routes that write through to the viewer PDS. */
export function pdsXrpcMethodForSocialWireGatewayRequest(
  gatewayMethod: string,
  gatewayPath: string
): { xrpcMethod: string; httpMethod: "GET" | "POST" } | null {
  const method = gatewayMethod.toUpperCase();
  const path = gatewayPath.startsWith("/") ? gatewayPath : `/${gatewayPath}`;

  if (method === "POST" && path === "/v1/appview/mark-all-read") {
    return { xrpcMethod: "com.atproto.repo.putRecord", httpMethod: "POST" };
  }
  return null;
}
