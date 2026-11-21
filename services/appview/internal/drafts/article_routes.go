package drafts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func (s *Service) Register(group *httpx.Group) {
	const prefix = "/xrpc/app.thesocialwire.articleDraft."
	group.Get(prefix+"list", s.articleRoute("list"))
	group.Get(prefix+"get", s.articleRoute("get"))
	group.Post(prefix+"put", s.articleRoute("put"))
	group.Post(prefix+"delete", s.articleRoute("delete"))
	s.registerSocial(group)
}

func (s *Service) articleRoute(operation string) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		owner := reqctx.ViewerDID(r.Context())
		if !validOwner(owner) {
			return httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "message": "Authentication required"})
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(60 * time.Second))
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(60 * time.Second))
		if s == nil {
			return draftFailure(w, errUnavailable)
		}
		release, err := s.acquire(owner)
		if err != nil {
			return draftFailure(w, err)
		}
		defer release()
		value, err := s.handleArticle(r, owner, operation)
		if err != nil {
			return draftFailure(w, err)
		}
		return httpx.OK(w, value)
	}
}

func (s *Service) handleArticle(r *http.Request, owner, operation string) (any, error) {
	ctx := r.Context()
	switch operation {
	case "list":
		limit := 20
		var err error
		if r.URL.Query().Has("limit") {
			limit, err = strconv.Atoi(r.URL.Query().Get("limit"))
		}
		cursor := r.URL.Query().Get("cursor")
		if err != nil || limit < 1 || limit > 20 || (r.URL.Query().Has("cursor") && !uuid.MatchString(cursor)) {
			return nil, errInvalid
		}
		return s.listArticles(ctx, owner, cursor, limit)
	case "get":
		id := r.URL.Query().Get("draftId")
		if !uuid.MatchString(id) {
			return nil, errInvalid
		}
		var result articleDocument
		err := s.db.Read(ctx, func(tx *sql.Tx) error {
			var err error
			result, _, err = s.article(ctx, tx, owner, id)
			return err
		})
		return result, err
	case "put", "delete":
		return s.articleMutation(r, owner, operation)
	default:
		return nil, errInvalid
	}
}

func (s *Service) articleMutation(r *http.Request, owner, operation string) (any, error) {
	var raw json.RawMessage
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return nil, errInvalid
	}
	if err := httpx.DecodeJSON(r, &raw, 2<<20); err != nil {
		if httpx.StatusOf(err) == http.StatusRequestEntityTooLarge {
			return nil, errQuota
		}
		return nil, errInvalid
	}
	var input map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&input); err != nil {
		return nil, errInvalid
	}

	fields := []string{"draftId", "expectedRevision", "mutationId"}
	if operation == "put" {
		fields = []string{"draft", "attachments", "expectedRevision", "mutationId"}
	}
	if !requiredFields(input, fields...) {
		return nil, errInvalid
	}
	var common struct {
		Revision int64  `json:"expectedRevision"`
		Mutation string `json:"mutationId"`
		ID       string `json:"draftId"`
	}
	if err := json.Unmarshal(encode(input), &common); err != nil || !uuid.MatchString(common.Mutation) || common.Revision < 0 || common.Revision == math.MaxInt64 {
		return nil, errInvalid
	}
	if operation == "delete" {
		if !uuid.MatchString(common.ID) {
			return nil, errInvalid
		}
		return s.mutateArticle(r.Context(), owner, common.ID, common.Mutation, common.Revision, nil, nil, nil, input)
	}
	draft, ok := input["draft"].(map[string]any)
	if !ok || !validArticleFields(draft, input["attachments"]) {
		return nil, errInvalid
	}
	payload := encode(draft)
	if len(payload) > 512<<10 {
		return nil, errQuota
	}
	var decoded articlePayload
	var refs []reference
	if json.Unmarshal(payload, &decoded) != nil || json.Unmarshal(encode(input["attachments"]), &refs) != nil || !decoded.valid(refs) {
		return nil, errInvalid
	}
	return s.mutateArticle(r.Context(), owner, decoded.ID, common.Mutation, common.Revision, &decoded, payload, refs, input)
}

func requiredFields(object map[string]any, keys ...string) bool {
	if len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if object[key] == nil {
			return false
		}
	}
	return true
}

func validArticleFields(draft map[string]any, source any) bool {
	required := []string{"id", "title", "markdown", "excerpt", "path", "tags", "publicationUri", "createdAt", "updatedAt", "assets"}
	allowed := map[string]bool{}
	for _, key := range required {
		if draft[key] == nil {
			return false
		}
		allowed[key] = true
	}
	for _, key := range []string{"coverAssetId", "publishedUri", "scheduleRecordKey", "scheduleUri", "publishingRecordKey", "publicationClaimId"} {
		allowed[key] = true
	}
	for key, value := range draft {
		if value == nil || !allowed[key] {
			return false
		}
	}
	return arrayFields(draft["assets"], "id", "alt", "width", "height", "mimeType", "name") &&
		arrayFields(source, "localAssetId", "assetId")
}

func arrayFields(raw any, fields ...string) bool {
	array, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range array {
		object, ok := item.(map[string]any)
		if !ok || !requiredFields(object, fields...) {
			return false
		}
	}
	return true
}

func draftFailure(w http.ResponseWriter, err error) error {
	status := http.StatusServiceUnavailable
	code, message := "Unavailable", "Private article draft storage unavailable"
	switch {
	case errors.Is(err, errInvalid):
		status, code, message = http.StatusBadRequest, "InvalidRequest", "Invalid private article draft request"
	case errors.Is(err, sql.ErrNoRows):
		status, code, message = http.StatusNotFound, "NotFound", "Private article draft not found"
	case errors.Is(err, errConflict):
		status, code, message = http.StatusConflict, "Conflict", "Private draft revision conflict"
	case errors.Is(err, errQuota):
		status, code, message = http.StatusRequestEntityTooLarge, "Quota", "Private article draft quota exceeded"
	case errors.Is(err, errPending):
		status, code, message = http.StatusConflict, "UploadPending", "Private draft attachment upload pending"
	}
	return httpx.JSON(w, status, map[string]string{"error": code, "message": message})
}
