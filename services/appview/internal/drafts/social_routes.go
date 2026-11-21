package drafts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func (s *Service) registerSocial(group *httpx.Group) {
	const prefix = "/xrpc/app.thesocialwire.socialDraft."
	for _, operation := range []string{"reserveAttachment", "uploadAttachment", "putManifest", "deleteManifest"} {
		group.Post(prefix+operation, s.socialRoute(operation))
	}
	for _, operation := range []string{"getAttachment", "getManifest"} {
		group.Get(prefix+operation, s.socialRoute(operation))
	}
}

func (s *Service) socialRoute(operation string) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		owner := reqctx.ViewerDID(r.Context())
		if !validOwner(owner) {
			return httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized", "message": "Authentication required"})
		}
		if s == nil {
			return socialFailure(w, errUnavailable)
		}
		transfer := operation == "uploadAttachment" || operation == "getAttachment"
		duration := 60 * time.Second
		if transfer {
			duration = 120 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), duration)
		defer cancel()
		r = r.WithContext(ctx)
		deadline, _ := ctx.Deadline()
		_ = http.NewResponseController(w).SetReadDeadline(deadline)
		_ = http.NewResponseController(w).SetWriteDeadline(deadline)
		release, err := s.acquire(owner)
		if err != nil {
			return socialFailure(w, err)
		}
		defer release()
		err = s.handleSocial(w, r, owner, operation)
		if err != nil {
			return socialFailure(w, err)
		}
		return nil
	}
}

func validOwner(owner string) bool {
	return strings.HasPrefix(owner, "did:") && len(owner) <= 2048 && !strings.ContainsAny(owner, "\x00\r\n") && owner != "did:web:anonymous.discovery"
}

func (s *Service) handleSocial(w http.ResponseWriter, r *http.Request, owner, operation string) error {
	ctx := r.Context()
	switch operation {
	case "reserveAttachment":
		var wanted asset
		if err := decodeSocial(r, &wanted, "assetId", "name", "contentType", "byteLength", "sha256", "kind"); err != nil {
			return err
		}
		result, err := s.reserve(ctx, owner, wanted)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]asset{"attachment": result})
	case "uploadAttachment":
		id := r.Header.Get("X-Social-Draft-Upload-Id")
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.ContentLength < 0 {
			return httpx.JSON(w, http.StatusLengthRequired, map[string]string{"error": "LengthRequired", "message": "Content-Length required"})
		}
		if r.ContentLength > maxFileBytes {
			return errQuota
		}
		if r.ContentLength == 0 || !uuid.MatchString(id) || media != "application/octet-stream" {
			return errInvalid
		}
		result, err := s.upload(ctx, owner, id, r.Body, r.ContentLength)
		if err != nil {
			return err
		}
		return httpx.OK(w, map[string]asset{"attachment": result})
	case "getAttachment":
		id := r.URL.Query().Get("assetId")
		if !uuid.MatchString(id) {
			return errInvalid
		}
		file, value, err := s.download(ctx, owner, id)
		if err != nil {
			return err
		}
		defer discard(file)
		w.Header().Set("Content-Type", value.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(value.ByteLength, 10))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, file)
		}
		return nil
	case "getManifest":
		id := r.URL.Query().Get("draftId")
		if !socialDraftID.MatchString(id) {
			return errInvalid
		}
		var result manifest
		err := s.db.Read(ctx, func(tx *sql.Tx) error {
			var err error
			result, err = s.readManifest(ctx, tx, owner, id)
			return err
		})
		if err != nil {
			return err
		}
		return httpx.OK(w, result)
	case "putManifest", "deleteManifest":
		var input manifestInput
		fields := []string{"draftId", "expectedRevision"}
		deleting := operation == "deleteManifest"
		if !deleting {
			fields = append(fields, "attachments")
		}
		if err := decodeSocial(r, &input, fields...); err != nil {
			return err
		}
		if input.Expected < 0 || input.Expected == math.MaxInt64 || !socialDraftID.MatchString(input.ID) || !validManifestRefs(input.Refs) {
			return errInvalid
		}
		result, err := s.putManifest(ctx, owner, input, deleting)
		if err != nil {
			return err
		}
		return httpx.OK(w, result)
	default:
		return errInvalid
	}
}

func decodeSocial(r *http.Request, result any, fields ...string) error {
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" {
		return errInvalid
	}
	var object map[string]json.RawMessage
	if err := httpx.DecodeJSON(r, &object, 1<<20); err != nil || len(object) != len(fields) {
		return errInvalid
	}
	for _, field := range fields {
		if value := object[field]; len(value) == 0 || string(value) == "null" {
			return errInvalid
		}
	}
	if json.Unmarshal(encode(object), result) != nil {
		return errInvalid
	}
	return nil
}

func socialFailure(w http.ResponseWriter, err error) error {
	status, code, message := http.StatusServiceUnavailable, "Unavailable", "Private draft storage unavailable"
	switch {
	case errors.Is(err, errInvalid):
		status, code, message = http.StatusBadRequest, "InvalidRequest", "Invalid private draft request"
	case errors.Is(err, sql.ErrNoRows):
		status, code, message = http.StatusNotFound, "NotFound", "Private draft attachment or manifest not found"
	case errors.Is(err, errConflict):
		status, code, message = http.StatusConflict, "Conflict", "Private draft revision conflict"
	case errors.Is(err, errRetired):
		status, code, message = http.StatusConflict, "AssetRetired", "Private draft attachment identifier retired"
	case errors.Is(err, errQuota):
		status, code, message = http.StatusRequestEntityTooLarge, "Quota", "Private draft quota exceeded"
	case errors.Is(err, errPending):
		status, code, message = http.StatusConflict, "UploadPending", "Private draft attachment upload pending"
	}
	return httpx.JSON(w, status, map[string]string{"error": code, "message": message})
}
