package repo

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVerifiesGenericLinksBytesIntegersAndDocumentsAboveReadStateLimit(t *testing.T) {
	link := HashCID([]byte{1, 2, 3})
	text := strings.Repeat("a", 70_000)
	object := map[string]any{
		"$type": "site.standard.document", "text": text,
		"blob":  map[string]any{"ref": map[string]any{"$link": link.String()}, "bytes": map[string]any{"$bytes": "AQID"}},
		"array": []any{true, nil, -42},
	}
	expected := MapValue(map[string]Value{
		"$type": StringValue("site.standard.document"), "text": StringValue(text),
		"blob":  MapValue(map[string]Value{"ref": LinkValue(link), "bytes": BytesValue([]byte{1, 2, 3})}),
		"array": ArrayValue([]Value{BoolValue(true), NullValue(), NegativeValue(41)}),
	})
	cid := HashCID(EncodeDAGCBOR(expected)).String()
	encoded, _ := json.Marshal(object)
	if err := VerifyPublicRecordCID(encoded, cid); err != nil {
		t.Fatal(err)
	}
	object["text"] = "Tampered"
	modified, _ := json.Marshal(object)
	if err := VerifyPublicRecordCID(modified, cid); err != ErrInvalidPublicRecord {
		t.Fatalf("err = %v", err)
	}
}

func TestEnforcesPublicRecordByteAndDepthLimits(t *testing.T) {
	cid := HashCID(nil).String()
	if err := VerifyPublicRecordCID(bytes.Repeat([]byte{32}, (1<<20)+1), cid); err == nil {
		t.Fatal("expected byte limit failure")
	}
	var value any = map[string]any{"a": "value"}
	for range 33 {
		value = map[string]any{"nested": value}
	}
	encoded, _ := json.Marshal(value)
	if err := VerifyPublicRecordCID(encoded, cid); err == nil {
		t.Fatal("expected depth failure")
	}
}
