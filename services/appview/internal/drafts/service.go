package drafts

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
)

var (
	uuid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	errInvalid     = errors.New("invalid draft")
	errConflict    = errors.New("draft revision conflict")
	errQuota       = errors.New("draft quota exceeded")
	errPending     = errors.New("draft upload pending")
	errUnavailable = errors.New("draft storage unavailable")
)

type storageConfig struct {
	Endpoint, Bucket, AccessKeyID, SecretAccessKey, Region string
}

type Service struct {
	objects  objectStore
	activeMu sync.Mutex
	active   map[string]bool

	db     *sqlitedb.DB
	cipher cipher.AEAD
}

// FromEnvironment loads the existing draft storage contract. Disabled storage
// is represented by a nil service.
func FromEnvironment(db *sqlitedb.DB, env config.Env) (*Service, error) {
	if !strings.EqualFold(env["SOCIAL_DRAFTS_ENABLED"], "true") {
		return nil, nil
	}
	endpoint, err := url.Parse(env["SOCIAL_DRAFT_S3_ENDPOINT"])
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return nil, errUnavailable
	}
	for _, key := range []string{"BUCKET", "ACCESS_KEY_ID", "SECRET_ACCESS_KEY", "REGION"} {
		if env["SOCIAL_DRAFT_S3_"+key] == "" {
			return nil, errUnavailable
		}
	}
	encoded := env["SOCIAL_DRAFT_STORAGE_KEY"]
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != encoded {
		return nil, errUnavailable
	}
	service, err := newService(db, key)
	if err != nil {
		return nil, err
	}
	service.objects = newS3Store(storageConfig{Endpoint: endpoint.String(),
		Bucket: env["SOCIAL_DRAFT_S3_BUCKET"], AccessKeyID: env["SOCIAL_DRAFT_S3_ACCESS_KEY_ID"],
		SecretAccessKey: env["SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY"], Region: env["SOCIAL_DRAFT_S3_REGION"],
	})
	return service, nil
}

func newService(db *sqlitedb.DB, key []byte) (*Service, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, cipher: gcm, active: map[string]bool{}}, nil
}

func encode(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
}

func (s *Service) openMetadata(owner, id string, data []byte) ([]byte, error) {
	size := s.cipher.NonceSize()
	if len(data) < size {
		return nil, errUnavailable
	}
	return s.cipher.Open(nil, data[:size], data[size:], encode([]any{"socialdraft", 1, owner, id, "metadata", 0}))
}

func (s *Service) sealMetadata(owner, id string, data []byte) ([]byte, error) {
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.cipher.Seal(nonce, nonce, data, encode([]any{"socialdraft", 1, owner, id, "metadata", 0})), nil
}

func (s *Service) acquire(owner string) (func(), error) {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if len(s.active) >= 8 || s.active[owner] {
		return nil, errUnavailable
	}
	s.active[owner] = true
	return func() {
		s.activeMu.Lock()
		delete(s.active, owner)
		s.activeMu.Unlock()
	}, nil
}
