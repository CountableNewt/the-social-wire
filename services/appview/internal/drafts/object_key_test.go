package drafts

import (
	"strings"
	"testing"
)

const legacyClipFixture = "clips/12345678-1234-1234-abcd-123456789abc/audio.m4a"

func TestObjectKeyCompatibilityIsNarrowAndDoesNotRelaxDigests(t *testing.T) {
	for _, key := range []string{strings.Repeat("a", 64), legacyClipFixture, strings.Replace(legacyClipFixture, "audio.m4a", "audiogram.mp4", 1)} {
		if !validObjectKey(key) {
			t.Fatal("valid object identity rejected")
		}
	}
	if assetDigest.MatchString(legacyClipFixture) {
		t.Fatal("object path accepted as cryptographic digest")
	}
	for _, key := range []string{"", strings.Repeat("a", 63), "/" + legacyClipFixture, legacyClipFixture + "/", strings.Replace(legacyClipFixture, "clips/", "clips//", 1), strings.Replace(legacyClipFixture, "clips/", "clips/../", 1), strings.Replace(legacyClipFixture, "clips/", "clips/./", 1), strings.Replace(legacyClipFixture, "clips/", "other/", 1), strings.Replace(legacyClipFixture, "audio.m4a", "audio.mp3", 1), strings.Replace(legacyClipFixture, "12345678", "1234567g", 1), strings.ReplaceAll(legacyClipFixture, "/", "\\"), legacyClipFixture + "?x=1", legacyClipFixture + "#x", legacyClipFixture + "\n", strings.Replace(legacyClipFixture, "audio.m4a", "%2e%2e", 1), strings.Replace(legacyClipFixture, "audio.m4a", "%252e%252e", 1), strings.ToUpper(legacyClipFixture)} {
		if validObjectKey(key) {
			t.Fatalf("unsafe object key accepted: %q", key)
		}
	}
}
