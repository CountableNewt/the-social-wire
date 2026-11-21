package atproto

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"strconv"
	"time"
)

// PreferencesRKey is the singleton preferences record key.
const PreferencesRKey = "self"

// GenerateTID mirrors DeterministicKeys.generateTID: the current Unix time in
// milliseconds rendered in lowercase base 32 (radix digits 0-9a-v).
func GenerateTID() string { return generateTIDAt(time.Now()) }

func generateTIDAt(now time.Time) string {
	return strconv.FormatUint(uint64(now.UnixMilli()), 32)
}

var upperBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// DeterministicRKey is base32(SHA-256(input)), uppercase RFC 4648 alphabet,
// no padding (52 characters) — the Stygian / L@tr record key scheme.
func DeterministicRKey(input string) string {
	sum := sha256.Sum256([]byte(input))
	return upperBase32.EncodeToString(sum[:])
}

// EntryReadStateRKey is the app.thesocialwire.entryReadState key for an entry AT-URI.
func EntryReadStateRKey(entryURI string) string { return DeterministicRKey(entryURI) }

// LatrItemRKey is the link.latr.saved.item key for a subject URI.
func LatrItemRKey(subjectURI string) string { return DeterministicRKey(subjectURI) }

// LatrExternalRKey is the L@tr key for a normalized external URL.
func LatrExternalRKey(normalizedURL string) string { return DeterministicRKey(normalizedURL) }

// LatrFingerprint is the lowercase hex SHA-256 of a normalized URL.
func LatrFingerprint(normalizedURL string) string {
	sum := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(sum[:])
}
