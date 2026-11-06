package gateway

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCanonicalPublicContractsRemainRegistered(t *testing.T) {
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if shipsSwiftGateway(string(dockerfile)) {
		t.Skip("Docker ships Swift Gateway; active contracts are covered by Swift HTTPRouteContractTests, ArticleMediaProxyTests, and Bun OpenAPI/client-XRPC transport tests; retained Go routes remain a rollback oracle")
	}
	seen := map[string]bool{}
	for _, route := range Routes {
		seen[route.Method+" "+route.Path] = true
	}
	for _, pattern := range []string{
		"GET /health", "GET /livez", "GET /readyz", "GET /freshness",
		"GET /oauth-client-metadata.json", "GET /oauth/client-metadata.json", "GET /ios-client-metadata.json", "GET /operations-oauth-client-metadata.json",
		"GET /v1/sync/preferences", "GET /xrpc/app.thesocialwire.sync.getPreferences", "POST /v1/sync/migrate-lexicons", "GET /v1/pds/cache/record", "POST /v1/telemetry/client-performance",
		"GET /v1/subscription/entitlements",
		"GET /v1/media/gifs", "GET /v1/media/gifs/thumbnail",
		"GET /xrpc/app.thesocialwire.socialDraft.getGifFile",
	} {
		seen[pattern] = true
	}
	raw, err := os.ReadFile("../../../../packages/spec/endpoint-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Entries []struct{ Surface, Method, Path string }
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, endpoint := range manifest.Entries {
		if endpoint.Surface != "openapi" {
			continue
		}
		count++
		pattern := endpoint.Method + " " + endpoint.Path
		if !seen[pattern] {
			t.Errorf("canonical Gateway contract missing %s", pattern)
		}
	}
	if count == 0 {
		t.Fatal("missing canonical public contracts")
	}
}

// Fail closed if the image selection is ambiguous. A Go rollback must still
// compare its route table with the current canonical public manifest.
func shipsSwiftGateway(dockerfile string) bool {
	var commands []string
	for _, line := range strings.Split(dockerfile, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			commands = append(commands, line)
		}
	}
	source := strings.Join(strings.Fields(strings.Join(commands, " ")), " ")
	return !strings.Contains(source, "go build") &&
		strings.Contains(source, "RUN swift build --package-path services/gateway -c release --product Gateway ") &&
		strings.Contains(source, "cp services/gateway/.build/release/Gateway /out/bin/Gateway") &&
		strings.Contains(source, "COPY --from=builder /out/bin/ /usr/local/bin/") &&
		strings.Contains(source, `ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/Gateway"]`)
}

func TestSwiftGatewayInventoryGuardPreservesGoRollback(t *testing.T) {
	swift := `RUN swift build --package-path services/gateway -c release --product Gateway --force-resolved-versions
  && cp services/gateway/.build/release/Gateway /out/bin/Gateway
COPY --from=builder /out/bin/ /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/schema-ready", "--", "/usr/local/bin/Gateway"]`
	for _, tc := range []struct {
		name, source string
		want         bool
	}{
		{"active Swift image", swift, true},
		{"Go rollback", "RUN go build -o /out/Gateway ./cmd/gateway\nENTRYPOINT [\"/usr/local/bin/schema-ready\", \"--\", \"/usr/local/bin/Gateway\"]", false},
		{"ambiguous Go build", swift + "\nRUN go build -o /out/Gateway ./cmd/gateway", false},
		{"unshipped Swift build", strings.ReplaceAll(swift, "COPY --from=builder /out/bin/ /usr/local/bin/", ""), false},
		{"Swift build in comment", "# " + strings.ReplaceAll(swift, "\n", "\n# "), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shipsSwiftGateway(tc.source); got != tc.want {
				t.Fatalf("image selects Swift = %v, want %v", got, tc.want)
			}
		})
	}
}
