package drafts

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxStoredObjectBytes = maxFileBytes + 1024*1024

var blobStoreID = regexp.MustCompile(`^[A-Za-z0-9]{1,63}$`)

// The HTTP contract follows the official @vercel/blob API client. Only already
// encrypted bytes enter this adapter. Object names are existing random keys.
type vercelBlobStore struct {
	client                        *http.Client
	token, storeID, privateOrigin string
	delegation                    *blobMigrationDelegation
}

func newVercelBlobStore(token string) (*vercelBlobStore, error) {
	parts := strings.Split(token, "_")
	if len(parts) != 5 || parts[0] != "vercel" || parts[1] != "blob" || parts[2] != "rw" || !blobStoreID.MatchString(parts[3]) || parts[4] == "" || strings.ContainsAny(token, " \t\r\n") {
		return nil, errUnavailable
	}
	return &vercelBlobStore{token: token, storeID: parts[3], privateOrigin: "https://" + strings.ToLower(parts[3]) + ".private.blob.vercel-storage.com",
		client: &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errUnavailable }}}, nil
}

func (s *vercelBlobStore) request(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	r, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errUnavailable
	}
	r.Header.Set("Authorization", "Bearer "+s.token)
	r.Header.Set("X-Api-Version", "13")
	r.Header.Set("X-Vercel-Blob-Store-Id", s.storeID)
	if s.delegation != nil {
		if err := s.delegation.signRequest(r); err != nil {
			return nil, errUnavailable
		}
		r.Header.Del("Authorization")
	}
	return r, nil
}

func (s *vercelBlobStore) objectURL(key string) (string, error) {
	if !assetDigest.MatchString(key) {
		return "", errUnavailable
	}
	return s.privateOrigin + "/" + key, nil
}

func (s *vercelBlobStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	return s.put(ctx, key, body, size, true)
}

func (s *vercelBlobStore) put(ctx context.Context, key string, body io.ReadSeeker, size int64, overwrite bool) error {
	if s.delegation != nil && (overwrite || size > s.delegation.maximumSize) {
		return errUnavailable
	}
	wantURL, err := s.objectURL(key)
	if err != nil || body == nil || size <= 0 || size > maxStoredObjectBytes {
		return errUnavailable
	}
	start, err := body.Seek(0, io.SeekCurrent)
	if err != nil {
		return errUnavailable
	}
	end, err := body.Seek(0, io.SeekEnd)
	if err != nil || end-start != size {
		return errUnavailable
	}
	if _, err := body.Seek(start, io.SeekStart); err != nil {
		return errUnavailable
	}
	r, err := s.request(ctx, http.MethodPut, "https://vercel.com/api/blob/?"+url.Values{"pathname": {key}}.Encode(), io.LimitReader(body, size))
	if err != nil {
		return err
	}
	r.ContentLength = size
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Content-Type", "application/octet-stream")
	r.Header.Set("X-Vercel-Blob-Access", "private")
	r.Header.Set("X-Add-Random-Suffix", "0")
	// Existing random keys are durable reservations. A retry must target the
	// same object, including when a previous response was lost after storage.
	r.Header.Set("X-Allow-Overwrite", "0")
	if overwrite {
		r.Header.Set("X-Allow-Overwrite", "1")
	}
	r.Header.Set("X-Cache-Control-Max-Age", "60")
	response, err := s.client.Do(r)
	if err != nil {
		return errUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return errUnavailable
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	if err != nil || len(encoded) > 16*1024 {
		return errUnavailable
	}
	var result struct{ URL, Pathname string }
	if json.Unmarshal(encoded, &result) != nil || result.Pathname != key || result.URL != wantURL {
		return errUnavailable
	}
	return nil
}

func (s *vercelBlobStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.get(ctx, key, false)
}

func (s *vercelBlobStore) get(ctx context.Context, key string, allowMissing bool) (io.ReadCloser, error) {
	endpoint, err := s.objectURL(key)
	if err != nil {
		return nil, err
	}
	r, err := s.request(ctx, http.MethodGet, endpoint+"?cache=0", nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept-Encoding", "identity")
	response, err := s.client.Do(r)
	if err != nil {
		return nil, errUnavailable
	}
	if allowMissing && response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return nil, os.ErrNotExist
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > maxStoredObjectBytes || (response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity") {
		response.Body.Close()
		return nil, errUnavailable
	}
	return &boundedBlobReader{body: response.Body, remaining: maxStoredObjectBytes}, nil
}

func (s *vercelBlobStore) Delete(ctx context.Context, key string) error {
	endpoint, err := s.objectURL(key)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string][]string{"urls": {endpoint}})
	if err != nil {
		return errUnavailable
	}
	r, err := s.request(ctx, http.MethodPost, "https://vercel.com/api/blob/delete", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(r)
	if err != nil {
		return errUnavailable
	}
	defer response.Body.Close()
	// Missing objects are already cleaned up; retries remain safe.
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode != http.StatusNotFound {
			return errUnavailable
		}
	}
	return nil
}

type boundedBlobReader struct {
	body      io.ReadCloser
	remaining int64
}

func (r *boundedBlobReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		var extra [1]byte
		n, err := r.body.Read(extra[:])
		if n != 0 {
			return 0, errUnavailable
		}
		return 0, err
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	n, err := r.body.Read(buffer)
	r.remaining -= int64(n)
	return n, err
}

func (r *boundedBlobReader) Close() error { return r.body.Close() }
