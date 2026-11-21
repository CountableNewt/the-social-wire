package operations

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestBoundedAttributesRemovesCredentialsAndBodies(t *testing.T) {
	result := BoundedAttributes(map[string]string{
		"route":         "/v1/appview/entries",
		"Authorization": "DPoP secret",
		"dpop_proof":    "secret",
		"recordBody":    "private",
	})
	if !maps.Equal(result, map[string]string{"route": "/v1/appview/entries"}) {
		t.Fatalf("got %v", result)
	}
	long := BoundedAttributes(map[string]string{"value": strings.Repeat("x", 300)})
	if len(long["value"]) != 256 {
		t.Fatalf("value not truncated: %d", len(long["value"]))
	}
}

func TestRecordIdentifierHashHidesSource(t *testing.T) {
	hashed := RecordIdentifierHash("at://did:plc:private/site.standard.document/secret")
	if strings.Contains(hashed, "did:plc") || len(hashed) != 24 {
		t.Fatalf("hash %q", hashed)
	}
}

type secretFailure struct{ message string }

func (e secretFailure) Error() string { return e.message }

func TestErrorCategoryUsesTypeNameOnly(t *testing.T) {
	if got := ErrorCategory(secretFailure{"database password"}); got != "secretfailure" {
		t.Fatalf("category %q", got)
	}
	if got := ErrorCategory(errors.New("password")); strings.Contains(got, "password") {
		t.Fatalf("category leaked message: %q", got)
	}
}
