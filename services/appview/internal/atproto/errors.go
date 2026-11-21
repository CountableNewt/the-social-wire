package atproto

import (
	"errors"
	"strconv"
)

// Identifier validation errors (StygianError).
var (
	ErrInvalidDID   = errors.New("atproto: invalid DID")
	ErrInvalidNSID  = errors.New("atproto: invalid NSID")
	ErrInvalidATURI = errors.New("atproto: invalid AT-URI")
)

// Public repository read errors (PublicRepoResolutionError).
var (
	// ErrRepoUnavailable means the repo's DID or PDS could not be resolved.
	ErrRepoUnavailable = errors.New("atproto: public repository unavailable")
	// ErrRepoMalformedResponse means the PDS answered with an invalid listRecords page.
	ErrRepoMalformedResponse = errors.New("atproto: malformed public repository response")
)

// Public network target errors (PDSAccessTokenAttestationError.invalid /
// .unavailable for PublicDNSAddressValidator and PublicMediaFetcher;
// RepositorySnapshotError "DNS rejected" / "DNS unavailable" for the
// repository validator).
var (
	// ErrPublicTargetInvalid rejects a URL or DNS answer by policy.
	ErrPublicTargetInvalid = errors.New("atproto: public target rejected")
	// ErrPublicTargetUnavailable is a transient failure: DNS failure, deadline,
	// admission overload, non-success status, or redirect budget exhausted.
	ErrPublicTargetUnavailable = errors.New("atproto: public target unavailable")
	// ErrBodyTooLarge reports a response body beyond the caller's byte bound.
	ErrBodyTooLarge = errors.New("atproto: response body exceeds limit")
)

// ErrUnsafeServiceEndpoint is ThinAppViewPdsResolutionError.unsafeServiceEndpoint:
// the DID document (or cache) names a PDS endpoint the policy rejects.
var ErrUnsafeServiceEndpoint = errors.New("atproto: unsafe PDS service endpoint")

// PLCUnavailableError means PLC could not be consulted (429 or 5xx), as distinct
// from PLC answering "this DID has no PDS". It is never negative-cached.
// (ATProtoPdsResolutionUnavailable / ThinAppViewPdsResolutionError.transientStatus.)
type PLCUnavailableError struct {
	Status int
}

func (e *PLCUnavailableError) Error() string {
	return "atproto: PLC unavailable (status " + strconv.Itoa(e.Status) + ")"
}
