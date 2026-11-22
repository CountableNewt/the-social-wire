package jobs

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

// Register mounts maintenance endpoints outside viewer OAuth admission. A missing
// secret disables HTTP execution while internal Railway runners remain available.
func Register(router *httpx.Router, runner *Runner, cronSecrets ...string) {
	cronSecret := ""
	if len(cronSecrets) > 0 {
		cronSecret = cronSecrets[0]
	}
	group := router.Group()
	group.Post("/internal/jobs/scheduling", handler(runner, "scheduling"))
	group.Post("/internal/jobs/drafts-cleanup", handler(runner, "drafts-cleanup"))
	group.Post("/internal/jobs/discovery", handler(runner, "discovery"))
	group.Post("/internal/jobs/ttl-cleanup", handler(runner, "ttl-cleanup"))
	group.Post("/internal/jobs/rss-poll", handler(runner, "rss-poll"))
	group.Post("/internal/jobs/analytics-cleanup", handler(runner, "analytics-cleanup"))
	group.Post("/internal/jobs/author-history", handler(runner, "author-history"))
	group.Post("/internal/jobs/read-state-recovery", handler(runner, "read-state-recovery"))
	group.Get("/internal/cron/scheduling", cronHandler(runner, "scheduling", cronSecret))
	group.Get("/internal/cron/drafts-cleanup", cronHandler(runner, "drafts-cleanup", cronSecret))
	group.Get("/internal/cron/discovery", cronHandler(runner, "discovery", cronSecret))
	group.Get("/internal/cron/ttl-cleanup", cronHandler(runner, "ttl-cleanup", cronSecret))
	group.Get("/internal/cron/rss-poll", cronHandler(runner, "rss-poll", cronSecret))
	group.Get("/internal/cron/analytics-cleanup", cronHandler(runner, "analytics-cleanup", cronSecret))
	group.Get("/internal/cron/author-history", cronHandler(runner, "author-history", cronSecret))
	group.Get("/internal/cron/read-state-recovery", cronHandler(runner, "read-state-recovery", cronSecret))
	group.Get("/internal/schedules/maintenance", cronHandler(runner, "maintenance", cronSecret))
}

// Cron authenticates a separate secret; accepting a viewer or manual-job token
// here would widen scheduler access. HEAD must never trigger a maintenance action.
func cronHandler(runner *Runner, name, secret string) httpx.Handler {
	return authorizedHandler(runner, name, secret, true)
}

func handler(runner *Runner, name string) httpx.Handler {
	secret := ""
	if runner != nil {
		secret = runner.secret
	}
	return authorizedHandler(runner, name, secret, false)
}

func authorizedHandler(runner *Runner, name, secret string, cron bool) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "no-store")
		if cron && r.Method != http.MethodGet {
			return httpx.Status(http.StatusMethodNotAllowed)
		}
		if runner == nil || !ValidSecret(secret) {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		authorization := r.Header.Values("Authorization")
		if len(authorization) != 1 || !strings.HasPrefix(authorization[0], "Bearer ") || len(authorization[0]) > 519 {
			return httpx.Status(http.StatusUnauthorized)
		}
		provided := sha256.Sum256([]byte(strings.TrimPrefix(authorization[0], "Bearer ")))
		expected := sha256.Sum256([]byte(secret))
		if subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 {
			return httpx.Status(http.StatusUnauthorized)
		}
		if cron && name == "maintenance" {
			result, err := runner.RunMaintenance(r.Context())
			if err != nil {
				return httpx.Status(http.StatusServiceUnavailable)
			}
			return httpx.OK(w, result)
		}
		result, err := runner.Run(r.Context(), name)
		if errors.Is(err, ErrUnavailable) {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		if err != nil {
			return httpx.Status(http.StatusServiceUnavailable)
		}
		return httpx.OK(w, result)
	}
}
