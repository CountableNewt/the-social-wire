package reader

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	turso "turso.tech/database/tursogo-serverless"
)

func TestLiveTursoCombinedFeedSnapshotIsReadOnly(t *testing.T) {
	dir := os.Getenv("TSW_TURSO_PROBE_DIR")
	if dir == "" {
		t.Skip("set TSW_TURSO_PROBE_DIR for isolated combined-query acceptance")
	}
	read := func(name string) string {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			t.Fatal("private regular probe credentials required")
		}
		value, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("probe credentials unavailable")
		}
		return strings.TrimSpace(string(value))
	}
	u, err := url.Parse(read("appview-url"))
	if err != nil || u.Hostname() != "tsw-dev-appview-probe-countablenewt.aws-us-west-2.turso.io" {
		t.Fatal("isolated AppView probe required")
	}
	u.Scheme = "https"
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return sqlitedb.ErrRemoteDatabase }}
	pool := sql.OpenDB(rehearsalDescribedConnector{turso.NewConnector(u.String(), read("appview-token")).WithHTTPClient(client).WithSessionRetention()})
	defer pool.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	// The test-only connector asks the server to verify the entire compound
	// statement is read-only before executing it. No fixture writes are needed.
	_, _, _, _, err = feedSnapshotWithMatches(ctx, pool, "did:example:absent-feed-probe", "subscribed", "")
	if !errors.Is(err, sql.ErrNoRows) {
		kind, code := sqlitedb.ErrorDiagnostic(err)
		t.Fatalf("combined snapshot compatibility failed: kind=%s code=%s", kind, code)
	}
}
