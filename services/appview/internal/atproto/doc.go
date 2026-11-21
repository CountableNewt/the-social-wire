// Package atproto is the shared AT Protocol client layer: identifier
// primitives (DID, NSID, AT-URI, TID, deterministic record keys), PLC/handle
// identity resolution with a process-wide cache, SSRF-safe public DNS
// validation and pinned fetching, and a PDS repo client that reads public
// repos or forwards the viewer's OAuth credentials (Authorization plus the
// upstream DPoP proof) for authenticated reads and writes.
//
// It ports GatewayCore's ATProtoAuthenticatedRepoClient / ATProtoPdsResolution /
// PublicDNSAddressValidator / PublicMediaFetcher / LexiconMigration,
// ThinAppViewCore's PDS resolution and transport helpers, and the
// ATProtoPrimitiveKit identifier types.
package atproto
