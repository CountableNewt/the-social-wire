export {
  createSaveUpstreamDpopProofPool,
  createUpstreamDpopProof,
  pdsXrpcMethodForGatewayRequest,
  primePdsDpopNonce,
  refreshPdsDpopNonce,
} from "latr-packages/gateway-client";

/** PDS XRPC method for Social Wire gateway routes that write through to the viewer PDS. */
export function pdsXrpcMethodForSocialWireGatewayRequest(
  _gatewayMethod: string,
  _gatewayPath: string
): { xrpcMethod: string; httpMethod: "GET" | "POST" } | null {
  void _gatewayMethod;
  void _gatewayPath;
  return null;
}
