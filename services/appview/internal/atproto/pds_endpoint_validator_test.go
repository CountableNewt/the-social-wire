package atproto

import "testing"

func TestPublicEndpointValidation(t *testing.T) {
	if got, ok := ValidatedPDSBase("https://pds.thesocialwire.social/", EndpointPublicHTTPS); !ok || got != "https://pds.thesocialwire.social" {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, endpoint := range []string{
		"http://pds.thesocialwire.social",
		"https://localhost",
		"https://pds.localhost",
		"https://127.0.0.1",
		"https://10.0.0.1",
		"https://169.254.169.254",
		"https://8.8.8.8",
		"https://[::1]",
		"https://metadata.google.internal",
		"https://pds.example.test",
		"https://user:password@pds.thesocialwire.social",
		"https://pds.thesocialwire.social?redirect=http://127.0.0.1",
		"https://pds.thesocialwire.social/xrpc",
		"https://pds.thesocialwire.social#frag",
	} {
		if got, ok := ValidatedPDSBase(endpoint, EndpointPublicHTTPS); ok {
			t.Errorf("unexpectedly accepted %s as %s", endpoint, got)
		}
	}
}

func TestLocalTestEndpointValidation(t *testing.T) {
	if got, ok := ValidatedPDSBase("http://127.0.0.1:8080/", EndpointLocalTesting); !ok || got != "http://127.0.0.1:8080" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := ValidatedPDSBase("http://localhost:8080", EndpointPublicHTTPS); ok {
		t.Fatal("public policy accepted localhost")
	}
	if _, ok := ValidatedPDSBase("http://192.168.1.10:8080", EndpointLocalTesting); ok {
		t.Fatal("local policy accepted private LAN")
	}
	if got, ok := ValidatedPDSBase("HTTPS://PDS.Example.Social./", EndpointPublicHTTPS); !ok || got != "https://pds.example.social" {
		t.Fatalf("normalization %q %v", got, ok)
	}
}
