package config

import "testing"

func TestRetiredOperationsClientIsNotImplicitlyAllowed(t *testing.T) {
	core, err := LoadCore(Env{
		"PDS_ATTESTATION_RECEIPT_SECRET":   "test-secret",
		"OAUTH_OPERATIONS_CLIENT_ID":       "https://operations.example/client-metadata.json",
		"OAUTH_GATEWAY_ALLOWED_CLIENT_IDS": "https://web.example/client-metadata.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, allowed := core.OAuthGateway.AllowedClientIDs["https://operations.example/client-metadata.json"]; allowed {
		t.Fatal("retired Operations client was implicitly authorized")
	}
	if _, allowed := core.OAuthGateway.AllowedClientIDs["https://web.example/client-metadata.json"]; !allowed {
		t.Fatal("explicitly configured client was removed")
	}
}
