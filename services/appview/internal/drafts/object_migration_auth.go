package drafts

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

// Delegated credentials are migration-only. The serving runtime still requires
// its private read-write token. This follows @vercel/blob signed-token.ts:
// delegation scope is checked locally, while Vercel authenticates its signature.
type blobMigrationDelegation struct {
	delegation, signingKey string
	validUntil             int64
	maximumSize            int64
}

func migrationBlobStoreFromEnvironment(env config.Env) (*vercelBlobStore, error) {
	path, id := env["BLOB_MIGRATION_SIGNED_TOKEN_FILE"], env["BLOB_MIGRATION_STORE_ID"]
	if path == "" && id == "" {
		return newVercelBlobStore(env["BLOB_READ_WRITE_TOKEN"])
	}
	if env["BLOB_READ_WRITE_TOKEN"] != "" || !filepath.IsAbs(path) {
		return nil, errUnavailable
	}
	id = strings.TrimPrefix(id, "store_")
	if !blobStoreID.MatchString(id) {
		return nil, errUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 64*1024 {
		return nil, errUnavailable
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errUnavailable
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0077 != 0 {
		return nil, errUnavailable
	}
	var issued struct {
		DelegationToken    string `json:"delegationToken"`
		ClientSigningToken string `json:"clientSigningToken"`
		ValidUntil         int64  `json:"validUntil"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 64*1024+1))
	if decoder.Decode(&issued) != nil || len(issued.DelegationToken) > 32*1024 {
		return nil, errUnavailable
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errUnavailable
	}
	parts := strings.Split(issued.DelegationToken, ".")
	if len(parts) != 2 {
		return nil, errUnavailable
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errUnavailable
	}
	var scope struct {
		StoreID             string   `json:"storeId"`
		Pathname            string   `json:"pathname"`
		Operations          []string `json:"operations"`
		ValidUntil          int64    `json:"validUntil"`
		MaximumSizeInBytes  int64    `json:"maximumSizeInBytes"`
		AllowedContentTypes []string `json:"allowedContentTypes"`
	}
	if json.Unmarshal(payload, &scope) != nil || strings.TrimPrefix(scope.StoreID, "store_") != id || scope.Pathname != "*" ||
		scope.ValidUntil != issued.ValidUntil || scope.ValidUntil <= time.Now().UnixMilli() || scope.ValidUntil > time.Now().Add(time.Hour+time.Minute).UnixMilli() ||
		scope.MaximumSizeInBytes <= 0 || scope.MaximumSizeInBytes > 106_000_000 || len(scope.AllowedContentTypes) != 0 || len(scope.Operations) != 2 {
		return nil, errUnavailable
	}
	operations := append([]string(nil), scope.Operations...)
	sort.Strings(operations)
	if operations[0] != "get" || operations[1] != "put" {
		return nil, errUnavailable
	}
	key, err := base64.RawURLEncoding.DecodeString(issued.ClientSigningToken)
	if err != nil || len(key) != 32 {
		return nil, errUnavailable
	}
	if signature, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil || len(signature) != 32 {
		return nil, errUnavailable
	}
	return &vercelBlobStore{storeID: id, privateOrigin: "https://" + strings.ToLower(id) + ".private.blob.vercel-storage.com",
		client:     &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errUnavailable }},
		delegation: &blobMigrationDelegation{issued.DelegationToken, issued.ClientSigningToken, issued.ValidUntil, scope.MaximumSizeInBytes}}, nil
}

func (d *blobMigrationDelegation) signRequest(r *http.Request) error {
	if time.Now().UnixMilli() >= d.validUntil {
		return errUnavailable
	}
	query := r.URL.Query()
	key, operation := strings.TrimPrefix(r.URL.Path, "/"), "get"
	if r.Method == http.MethodPut {
		key, operation = query.Get("pathname"), "put"
		query.Set("vercel-blob-add-random-suffix", "false")
		query.Set("vercel-blob-allow-overwrite", "false")
		query.Set("vercel-blob-cache-control-max-age", "60")
	} else if r.Method != http.MethodGet {
		return errUnavailable
	}
	if !assetDigest.MatchString(key) {
		return errUnavailable
	}
	lines := []string{"operation=" + operation, "pathname=" + key}
	for _, name := range []string{"vercel-blob-add-random-suffix", "vercel-blob-allow-overwrite", "vercel-blob-cache-control-max-age"} {
		if value := query.Get(name); value != "" {
			lines = append(lines, name+"="+value)
		}
	}
	sort.Strings(lines)
	mac := hmac.New(sha256.New, []byte(d.signingKey))
	mac.Write([]byte(strings.Join(lines, "\n")))
	query.Set("vercel-blob-delegation", d.delegation)
	query.Set("vercel-blob-signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	r.URL.RawQuery = query.Encode()
	return nil
}
