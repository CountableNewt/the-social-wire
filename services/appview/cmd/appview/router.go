package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/articles"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/auth"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/drafts"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/edge"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/operations"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reader"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/semble"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/syncapi"
)

type runtimeServices struct {
	db            *sqlitedb.DB
	repo          *atproto.RepoClient
	operations    operations.Configuration
	telemetry     *operations.TelemetryBuffer
	enroll        reader.EnrollFunc
	readerOptions reader.Options
	drafts        *drafts.Service
	analytics     *analytics.Store
	scheduling    *scheduler.Service
	maintenance   *jobrunner.Runner
	context       context.Context
	logger        *slog.Logger
	serverless    bool
}

func buildRouter(core config.Core, env config.Env, services runtimeServices) (http.Handler, error) {
	verifier, err := auth.New(core, services.repo.Resolver())
	if err != nil {
		return nil, err
	}
	router := httpx.NewRouter()
	if services.serverless {
		router.Use(operations.RequestTelemetry(services.telemetry))
	}
	router.Use(operations.RequestTrace("appview", services.operations.Environment, services.operations.InstanceID, services.telemetry))
	router.Use(edge.CORS(core))
	checks := readinessChecks{draftsEnabled: services.drafts != nil}
	if services.scheduling != nil {
		checks.scheduling = services.scheduling.Ready
	}
	registerHealth(router, core, services.db, services.logger, checks)
	edge.RegisterMetadata(router, core)
	jobrunner.Register(router, services.maintenance, env.String("CRON_SECRET"))

	admission := edge.NewAdmission(core)
	protected := router.Group(httpx.XRPCErrors(), verifier.Middleware, admission.Middleware)
	edge.RegisterSubscriptions(protected, core)
	edge.RegisterLatr(protected, env, services.repo.HTTPClient())
	edge.RegisterGIF(protected, core, admission)
	services.drafts.Register(protected)
	articles.Register(protected, services.repo)
	semble.Register(protected, env, services.repo)
	syncapi.Register(protected, services.db, services.repo)
	operations.RegisterClientPerformance(protected, services.telemetry, services.operations.Environment)

	publicationsService := publications.New(services.db, services.repo)
	readState := reader.Register(protected, services.db, publicationsService, services.enroll, services.readerOptions)
	publications.Register(protected.Use(readState), publicationsService)
	if services.analytics != nil {
		analytics.Register(protected, services.analytics, services.repo)
	}
	if services.scheduling != nil {
		services.scheduling.Register(router, protected)
	}
	return router, nil
}

type readinessChecks struct {
	draftsEnabled bool
	scheduling    func(context.Context) error
}

func registerHealth(router *httpx.Router, core config.Core, db *sqlitedb.DB, logger *slog.Logger, checks readinessChecks) {
	router.Get("/health", healthStatus("ok"))
	router.Get("/livez", healthStatus("live"))
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) error {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			logger.Warn("AppView database readiness check failed")
			return httpx.Status(http.StatusServiceUnavailable)
		}
		if core.DatabaseBackend == "turso" {
			if err := verifyRuntimeSchemaMarker(ctx, db, checks.draftsEnabled); err != nil {
				logger.Warn("AppView schema readiness check failed")
				return httpx.Status(http.StatusServiceUnavailable)
			}
		}
		if checks.scheduling != nil {
			if err := checks.scheduling(ctx); err != nil {
				logger.Warn("Scheduling database readiness check failed")
				return httpx.Status(http.StatusServiceUnavailable)
			}
		}
		return healthStatus("ready")(w, r)
	})
	router.Get("/freshness", operations.FreshnessHandler("appview"))
	router.Get("/xrpc/app.thesocialwire.teapot", func(w http.ResponseWriter, _ *http.Request) error {
		return httpx.JSON(w, http.StatusTeapot, map[string]string{"error": "Teapot", "message": "I'm a teapot"})
	})
}

func healthStatus(status string) httpx.Handler {
	return func(w http.ResponseWriter, _ *http.Request) error {
		return httpx.OK(w, map[string]string{"status": status, "service": "appview"})
	}
}

// Validate the address selected for every connection to a PLC or PDS endpoint.
// Keep the original request host for TLS and HTTP authority checks.
func publicHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(target)
			if err != nil {
				return nil, err
			}
			address, err := atproto.PublicDNS.ValidatedAddress(ctx, "https://"+target, time.Now().Add(5*time.Second))
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(address, port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
