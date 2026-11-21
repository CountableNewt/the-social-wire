package reader

import (
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func TestListShareURLAndBoundCursor(t *testing.T) {
	id, ok := parseListURI("https://standard-reader.app/l/did%3Aplc%3Aauthor/news?view=compact#feed", true)
	if !ok || id.String() != "at://did:plc:author/app.standard-reader.list/news" {
		t.Fatalf("share identity: %+v", id)
	}
	for _, input := range []string{"https://standard-reader.app@example.org/l/did:plc:author/news",
		"https://standard-reader.app/l/did:plc:author/a%2Fb", "at://did:plc:author/app.standard-reader.list/.."} {
		if _, ok := parseListURI(input, true); ok {
			t.Fatalf("invalid share URL accepted: %s", input)
		}
	}
	list, valid := decodeList(id, map[string]any{
		"name": "News", "createdAt": "2026-01-01T00:00:00Z", "publications": []any{testPub, testPub},
		"users": []any{"did:plc:another"},
	}, testViewer, true)
	if !valid || list.Owned || !list.Saved || len(list.Publications) != 1 || len(list.Users) != 1 {
		t.Fatalf("list metadata: %+v", list)
	}
	token := thinstore.EncodeCursor(time.Now(), "entry")
	fingerprint := listFingerprint(testViewer, list, thinstore.FilterAll)
	encoded := encodeListCursor(&token, fingerprint)
	got, err := decodeListCursor(*encoded, fingerprint)
	if err != nil || got == nil || *got != token {
		t.Fatalf("cursor round trip: %v %v", got, err)
	}
	for _, other := range []string{listFingerprint(testViewer, list, thinstore.FilterRead), listFingerprint("did:plc:another", list, thinstore.FilterAll)} {
		if _, err := decodeListCursor(*encoded, other); err == nil {
			t.Fatal("cursor accepted under different viewer or filter")
		}
	}
}
