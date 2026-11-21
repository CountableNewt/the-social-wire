package atproto

import (
	"errors"
	"testing"
	"time"
)

func TestParseATURI(t *testing.T) {
	uri, ok := ParseATURI("at://did:plc:abc123/com.example.collection/rkey456")
	if !ok || uri.Repo != "did:plc:abc123" || uri.Collection != "com.example.collection" || uri.RKey != "rkey456" {
		t.Fatalf("unexpected parse %+v %v", uri, ok)
	}
	if uri.String() != "at://did:plc:abc123/com.example.collection/rkey456" {
		t.Fatalf("round trip %q", uri.String())
	}
	for _, raw := range []string{"https://example.test", "at://did:plc:abc"} {
		if _, ok := ParseATURI(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestParseRecordURIIsStrict(t *testing.T) {
	if _, ok := ParseRecordURI("at://did:plc:a/c/k/extra"); ok {
		t.Fatal("strict parser accepted extra segment")
	}
	if got, ok := ParseRecordURI("at://did:plc:a/c/k"); !ok || got.RKey != "k" {
		t.Fatalf("strict parse %+v %v", got, ok)
	}
	if RKeyFromURI("at://did:plc:a/c/k") != "k" || RKeyFromURI("") != "" {
		t.Fatal("rkey extraction")
	}
	repo, pub := RepoAndPublicationFilter("at://did:plc:a/site.standard.publication/p")
	if repo != "did:plc:a" || pub != "at://did:plc:a/site.standard.publication/p" {
		t.Fatalf("filter %q %q", repo, pub)
	}
	repo, pub = RepoAndPublicationFilter("did:plc:a")
	if repo != "did:plc:a" || pub != "" {
		t.Fatalf("filter %q %q", repo, pub)
	}
}

func TestNormalizeDID(t *testing.T) {
	got, err := NormalizeDID("  did:plc:abc123  ")
	if err != nil || got != "did:plc:abc123" {
		t.Fatalf("normalize %q %v", got, err)
	}
	for _, raw := range []string{"plc:abc123", "did:plc", "did::x", "did:plc:"} {
		if _, err := NormalizeDID(raw); !errors.Is(err, ErrInvalidDID) {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestParseNSID(t *testing.T) {
	nsid, ok := ParseNSID("com.example.collection")
	if !ok || nsid.Authority != "com.example" || nsid.Name != "collection" {
		t.Fatalf("parse %+v", nsid)
	}
	if _, ok := ParseNSID("com.example"); ok {
		t.Fatal("accepted two labels")
	}
	if got, _ := NormalizeNSID(" COM.Example.Thing "); got != "com.example.thing" {
		t.Fatalf("normalize %q", got)
	}
}

func TestBase64URL(t *testing.T) {
	encoded := EncodeBase64URL([]byte{0xfb, 0xff, 0x01})
	if encoded != "-_8B" {
		t.Fatalf("encode %q", encoded)
	}
	decoded, err := DecodeBase64URL("-_8B")
	if err != nil || len(decoded) != 3 {
		t.Fatalf("decode %v %v", decoded, err)
	}
	if _, err := DecodeBase64URL("a"); err == nil {
		t.Fatal("decoded invalid length")
	}
}

func TestRepoCollectionScope(t *testing.T) {
	scope := RepoCollectionScope("com.example.collection", nil)
	if scope != "repo:com.example.collection?create,update,delete" {
		t.Fatalf("scope %q", scope)
	}
	if got, ok := ParseRepoCollectionScope(scope); !ok || got != "com.example.collection" {
		t.Fatalf("parse %q", got)
	}
}

// Stygian cross-language golden vectors (latr-packages record-keys fixture).
func TestDeterministicKeysGoldenVectors(t *testing.T) {
	if got := EntryReadStateRKey("at://did:plc:alice/site.standard.document/abc123"); got != "JPFAJWZIZ7VWQJ3CR2L7PEPRNZBZ6LJ7MKKO3RKWB642BF64NBXQ" {
		t.Fatalf("entry read state rkey %q", got)
	}
	if got := LatrExternalRKey("https://example.com/article"); got != "MMSTQKIENDT2HHAGGI6J4OXJR4YQOLLEDS5TP2RXSF7VNO7LKU4Q" {
		t.Fatalf("external rkey %q", got)
	}
	if got := LatrFingerprint("https://example.com/article"); got != "632538290468e7a39c06323c9e3ae98f31072d641cbb37ea37917f56bbeb5539" {
		t.Fatalf("fingerprint %q", got)
	}
}

func TestGenerateTIDIsBase32Milliseconds(t *testing.T) {
	if got := generateTIDAt(time.UnixMilli(1_700_000_000_000)); got != "1hf7uaq00" {
		t.Fatalf("tid %q", got)
	}
}

func TestLegacyCollectionPairs(t *testing.T) {
	pairs := LegacyCollections()
	if len(pairs) != 3 || pairs[0].Legacy != LegacyCollectionFolder || pairs[0].Current != CollectionFolder {
		t.Fatalf("pairs %+v", pairs)
	}
}
