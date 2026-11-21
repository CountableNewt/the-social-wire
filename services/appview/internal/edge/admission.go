package edge

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

type ownerAdmission struct {
	requests  float64
	transfers float64
	updated   time.Time
	active    int
}

// Admission bounds concurrent draft transfers and enforces tool entitlements.
type Admission struct {
	core   config.Core
	mu     sync.Mutex
	owners map[string]ownerAdmission
	active int
}

// NewAdmission creates the process-local standalone draft admission.
func NewAdmission(core config.Core) *Admission {
	return &Admission{core: core, owners: make(map[string]ownerAdmission)}
}

func (a *Admission) acquire(owner string, transfer bool, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, exists := a.owners[owner]
	if !exists {
		if len(a.owners) >= 20_000 {
			for key, existing := range a.owners {
				if existing.active == 0 && now.Sub(existing.updated) > 2*time.Minute {
					delete(a.owners, key)
				}
			}
		}
		if len(a.owners) >= 20_000 {
			return false
		}
		state = ownerAdmission{requests: 120, transfers: 32, updated: now}
	}
	elapsed := max(0, now.Sub(state.updated).Seconds())
	state.requests = min(120, state.requests+10*elapsed)
	state.transfers = min(32, state.transfers+elapsed)
	if state.requests < 1 || (transfer && (a.active >= 8 || state.active >= 2 || state.transfers < 1)) {
		return false
	}
	state.requests--
	state.updated = now
	if transfer {
		a.active++
		state.active++
		state.transfers--
	}
	a.owners[owner] = state
	return true
}

func (a *Admission) release(owner string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.owners[owner]
	state.active--
	a.owners[owner] = state
	a.active--
}

// Middleware applies edge admission after authentication.
func (a *Admission) Middleware(next httpx.Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		did := reqctx.ViewerDID(r.Context())
		path := r.URL.Path
		if a.core.ProGatingEnabled && requiresPro(path) && !a.core.HasPro(did) {
			w.Header().Set("Cache-Control", "private, no-store")
			return httpx.JSON(w, http.StatusForbidden, map[string]string{"error": "ProRequired", "message": "A Pro subscription is required"})
		}
		if (!strings.HasPrefix(path, "/xrpc/app.thesocialwire.articleDraft.") &&
			!strings.HasPrefix(path, "/xrpc/app.thesocialwire.socialDraft.")) || strings.HasSuffix(path, ".getGifFile") {
			return next(w, r)
		}
		if did == "" {
			return httpx.ErrUnauthorized
		}
		transfer := strings.HasSuffix(path, ".uploadAttachment") || strings.HasSuffix(path, ".getAttachment")
		if !a.acquire(did, transfer, time.Now()) {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return nil
		}
		if transfer {
			defer a.release(did)
		}
		return next(w, r)
	}
}

func requiresPro(path string) bool {
	switch path {
	case "/v1/analytics/summary", "/v1/analytics/refresh", "/v1/analytics/bluesky", "/v1/analytics/publications":
		return true
	}
	return strings.HasPrefix(path, "/xrpc/at.skej.")
}

// RegisterSubscriptions exposes the signed-in viewer's plan.
func RegisterSubscriptions(group *httpx.Group, core config.Core) {
	group.Get("/v1/subscription/entitlements", func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "private, no-store")
		did := reqctx.ViewerDID(r.Context())
		if did == "" {
			return httpx.ErrUnauthorized
		}
		plan := "free"
		if core.HasPro(did) {
			plan = "pro"
		}
		return httpx.JSON(w, http.StatusOK, map[string]string{"viewerDid": did, "plan": plan})
	})
}
