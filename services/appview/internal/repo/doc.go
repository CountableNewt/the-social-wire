// Package repo verifies ATProto repository data: DAG-CBOR values, CIDs, CAR
// archives, Merkle Search Trees, signed commits, and full repository snapshots
// fetched block-by-block from a publisher's PDS (com.atproto.sync.*). It also
// hosts the DNS address validator used to pin untrusted HTTPS endpoints to
// globally routable addresses, and content-address checks for public records.
package repo
