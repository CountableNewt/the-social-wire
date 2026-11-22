package drafts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

type blobTestTransport func(*http.Request) (*http.Response, error)

func (f blobTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func blobTestStore(t *testing.T, transport blobTestTransport) *vercelBlobStore {
	t.Helper()
	s, err := newVercelBlobStore("vercel_blob_rw_teststore_testsecret")
	if err != nil {
		t.Fatal(err)
	}
	s.client.Transport = transport
	return s
}

func blobTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

func TestPrivateBlobRoundTripAndIdempotentKeys(t *testing.T) {
	key := strings.Repeat("a", 64)
	ciphertext := []byte("encrypted-payload-not-a-publisher-file")
	var stored []byte
	var puts, deletes int
	s := blobTestStore(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer vercel_blob_rw_teststore_testsecret" || r.Header.Get("X-Api-Version") != "13" || r.Header.Get("X-Vercel-Blob-Store-Id") != "teststore" {
			t.Fatal("missing scoped authentication")
		}
		switch r.Method {
		case http.MethodPut:
			puts++
			if r.URL.String() != "https://vercel.com/api/blob/?pathname="+key || r.Header.Get("X-Vercel-Blob-Access") != "private" || r.Header.Get("X-Add-Random-Suffix") != "0" || r.Header.Get("X-Allow-Overwrite") != "1" || r.Header.Get("X-Content-Type") != "application/octet-stream" || r.ContentLength != int64(len(ciphertext)) {
				t.Fatal("upload did not preserve private durable object identity")
			}
			stored, _ = io.ReadAll(r.Body)
			if !bytes.Equal(stored, ciphertext) {
				t.Fatal("ciphertext changed")
			}
			return blobTestResponse(200, `{"url":"https://teststore.private.blob.vercel-storage.com/`+key+`","pathname":"`+key+`"}`), nil
		case http.MethodGet:
			if r.URL.String() != "https://teststore.private.blob.vercel-storage.com/"+key+"?cache=0" || r.Header.Get("Accept-Encoding") != "identity" {
				t.Fatal("download did not use uncached private authenticated origin")
			}
			return blobTestResponse(200, string(stored)), nil
		case http.MethodPost:
			deletes++
			var payload struct {
				URLs []string `json:"urls"`
			}
			if r.URL.String() != "https://vercel.com/api/blob/delete" || json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.URLs) != 1 || payload.URLs[0] != "https://teststore.private.blob.vercel-storage.com/"+key {
				t.Fatal("delete targeted a different object")
			}
			return blobTestResponse(200, `{}`), nil
		}
		t.Fatal("unexpected request")
		return nil, errUnavailable
	})
	for range 2 {
		if err := s.Put(t.Context(), key, bytes.NewReader(ciphertext), int64(len(ciphertext))); err != nil {
			t.Fatal(err)
		}
	}
	body, err := s.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(body)
	if err != nil || !bytes.Equal(got, ciphertext) {
		t.Fatal("private download mismatch", err)
	}
	body.Close()
	for range 2 {
		if err := s.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
	}
	if puts != 2 || deletes != 2 {
		t.Fatal("retries changed operation identity")
	}
}

func TestPrivateBlobRejectsRedirectsAndUnexpectedPutResponse(t *testing.T) {
	key := strings.Repeat("b", 64)
	for _, redirect := range []bool{false, true} {
		calls := 0
		s := blobTestStore(t, func(r *http.Request) (*http.Response, error) {
			calls++
			if calls > 1 {
				t.Fatal("followed an untrusted response URL")
			}
			if redirect {
				response := blobTestResponse(307, "")
				response.Header.Set("Location", "https://attacker.example/private")
				return response, nil
			}
			return blobTestResponse(200, `{"url":"https://teststore.public.blob.vercel-storage.com/`+key+`","pathname":"`+key+`"}`), nil
		})
		if err := s.Put(t.Context(), key, bytes.NewReader([]byte{1}), 1); !errors.Is(err, errUnavailable) {
			t.Fatal("accepted redirect/public response", err)
		}
	}
}

func TestPrivateBlobBoundsAndCancellation(t *testing.T) {
	key := strings.Repeat("c", 64)
	calls := 0
	s := blobTestStore(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		response := blobTestResponse(200, "")
		response.ContentLength = maxStoredObjectBytes + 1
		return response, nil
	})
	if _, err := s.Get(t.Context(), key); !errors.Is(err, errUnavailable) {
		t.Fatal("oversized object accepted")
	}
	for _, invalid := range []string{"../secret", "https://attacker.example/key", strings.Repeat("a", 63)} {
		if _, err := s.Get(t.Context(), invalid); err == nil {
			t.Fatal("untrusted key accepted")
		}
	}
	if calls != 1 {
		t.Fatal("invalid key reached network")
	}
	if err := s.Put(t.Context(), key, bytes.NewReader([]byte{1}), 2); err == nil || calls != 1 {
		t.Fatal("mismatched upload length reached network")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Get(ctx, key); err == nil {
		t.Fatal("ignored cancellation")
	}
	reader := &boundedBlobReader{body: io.NopCloser(strings.NewReader("1234")), remaining: 3}
	if got, err := io.ReadAll(reader); string(got) != "123" || !errors.Is(err, errUnavailable) {
		t.Fatal("unbounded stream", string(got), err)
	}
}

func TestPrivateBlobEnvironmentRequiresExplicitBackendAndKey(t *testing.T) {
	env := config.Env{"SOCIAL_DRAFTS_ENABLED": "true", "SOCIAL_DRAFT_STORAGE_BACKEND": "vercel-blob", "BLOB_READ_WRITE_TOKEN": "vercel_blob_rw_teststore_testsecret", "SOCIAL_DRAFT_STORAGE_KEY": base64.StdEncoding.EncodeToString(make([]byte, 32))}
	s, err := FromEnvironment(nil, env)
	if err != nil || s == nil {
		t.Fatal("valid private Blob configuration", err)
	}
	if _, ok := s.objects.(*vercelBlobStore); !ok {
		t.Fatal("wrong object backend")
	}
	for _, token := range []string{"", "vercel_blob_rw_/bad_token", "vercel_blob_rw_teststore_secret\n"} {
		env["BLOB_READ_WRITE_TOKEN"] = token
		if _, err := FromEnvironment(nil, env); err == nil {
			t.Fatal("invalid Blob credentials accepted")
		}
	}
	env["SOCIAL_DRAFT_STORAGE_BACKEND"] = "unknown"
	if _, err := FromEnvironment(nil, env); err == nil {
		t.Fatal("unknown backend accepted")
	}
}

func TestPrivateBlobLegacyKeysStayPrivateAndExact(t *testing.T) {
	key := legacyClipFixture
	calls := 0
	s := blobTestStore(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method == http.MethodGet {
			if r.URL.Path != "/"+key || r.URL.Host != "teststore.private.blob.vercel-storage.com" {
				t.Fatal("legacy path changed")
			}
			return blobTestResponse(200, "ciphertext"), nil
		}
		if r.Method == http.MethodPost {
			var p struct {
				URLs []string `json:"urls"`
			}
			if json.NewDecoder(r.Body).Decode(&p) != nil || len(p.URLs) != 1 || p.URLs[0] != "https://teststore.private.blob.vercel-storage.com/"+key {
				t.Fatal("legacy delete changed identity")
			}
			return blobTestResponse(200, `{}`), nil
		}
		if r.Method == http.MethodPut {
			if r.URL.Query().Get("pathname") != key || r.Header.Get("X-Allow-Overwrite") != "0" {
				t.Fatal("legacy migration changed immutable path")
			}
			return blobTestResponse(200, `{"pathname":"`+key+`","url":"https://teststore.private.blob.vercel-storage.com/`+key+`"}`), nil
		}
		t.Fatal("unexpected method")
		return nil, errUnavailable
	})
	b, err := s.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	if err := s.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if err := (migrationBlobDestination{s}).Create(t.Context(), key, bytes.NewReader([]byte{1}), 1); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../" + key, key + "?x=1", strings.Replace(key, "clips/", "clips/%2e%2e/", 1)} {
		if _, err := s.Get(t.Context(), bad); err == nil {
			t.Fatal("unsafe path reached network")
		}
		if err := s.Delete(t.Context(), bad); err == nil {
			t.Fatal("unsafe delete reached network")
		}
	}
	if calls != 3 {
		t.Fatal("unsafe key reached network")
	}
}
