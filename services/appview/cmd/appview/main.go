package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/drafts"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/ingest"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/operations"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/podcasts"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/publications"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reader"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/sqlitedb"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/syncapi"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, os.Args[1:], config.Load(), logger); err != nil {
		logger.Error("AppView stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, env config.Env, logger *slog.Logger) error {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	opts := flag.NewFlagSet("AppView", flag.ContinueOnError)
	initializeOnly := opts.Bool("initialize-only", false, "initialize SQLite schemas and exit")
	port := opts.String("port", "", "override the HTTP port")
	host := opts.String("hostname", "", "override the HTTP bind address")
	if err := opts.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if opts.NArg() != 0 {
		return errors.New("unexpected AppView argument")
	}
	if *port != "" {
		env["PORT"] = *port
	}
	if *host != "" {
		env["BIND_HOST"] = *host
	}
	core, err := config.LoadCore(env)
	if err != nil {
		return err
	}
	if !env.True("ENABLE_THIN_APPVIEW") {
		return errors.New("ENABLE_THIN_APPVIEW must be true for AppView")
	}
	discoveryMode := env.StringOr("PUBLIC_CONTENT_DISCOVERY_MODE", "microcosm")
	if discoveryMode != "microcosm" && discoveryMode != "off" {
		return errors.New("PUBLIC_CONTENT_DISCOVERY_MODE must be microcosm or off")
	}
	environment, err := operations.RequireEnvironment(env)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(core.SQLitePath), 0700); err != nil {
		return fmt.Errorf("create SQLite directory: %w", err)
	}
	db, err := sqlitedb.Open(core.SQLitePath)
	if err != nil {
		return err
	}
	defer db.Close()
	draftService, err := drafts.FromEnvironment(db, env)
	if err != nil {
		return fmt.Errorf("configure draft storage: %w", err)
	}
	if err := initialize(ctx, db, core, draftService != nil); err != nil {
		return err
	}
	client := publicHTTPClient()
	defer client.CloseIdleConnections()
	schedulingService, err := scheduler.Open(ctx, core, env, client, logger)
	if err != nil {
		return fmt.Errorf("initialize embedded scheduling: %w", err)
	}
	if schedulingService != nil {
		defer schedulingService.Close()
	}
	if *initializeOnly {
		logger.Info("Standalone SQLite schemas initialized")
		return nil
	}

	ops := operations.LoadConfiguration(env)
	store := operations.NewStore(db, environment, logger)
	var telemetry *operations.TelemetryBuffer
	if ops.Enabled {
		telemetry = operations.NewTelemetryBuffer(store, logger)
	}
	repo := atproto.NewRepoClient(client, core.PLCURL, logger)
	contentStore := thinstore.SQLiteIngestStore{DB: db}
	retention := configuredSeconds(env, "THIN_APPVIEW_CONTENT_TTL_SECONDS", 30*24*time.Hour)
	rssLimit, _ := strconv.Atoi(env.String("THIN_APPVIEW_MAX_RSS_ITEMS_PER_FEED"))
	ingestion := &ingest.Service{Repo: repo, Store: contentStore, ContentRetention: retention, MaximumRSSItems: rssLimit}
	entryVerifier := ingest.NewEntryVerifier(db, core.PLCURL, retention)
	var discovery *ingest.Microcosm
	if discoveryMode == "off" {
		ingestion.AuthorPDS = repo.Resolver()
	} else {
		discovery = ingest.NewMicrocosm(client, contentStore)
	}
	var analyticsStore *analytics.Store
	if core.ToolsEnabled {
		analyticsStore = analytics.NewStore(db, core.GatewayAppViewInternalSecret)
	}
	background, stop := context.WithCancel(ctx)
	var jobs sync.WaitGroup
	defer func() {
		stop()
		jobs.Wait()
	}()
	handler, err := buildRouter(core, env, runtimeServices{
		db: db, repo: repo, operations: ops, telemetry: telemetry,
		enroll:        enrollment(db, ingestion, discovery),
		readerOptions: reader.Options{Bootstrap: bootstrapDiscovery(discovery), VerifyEntry: entryVerifier.Verify, RefreshScope: refreshScope(ingestion, discovery)},
		drafts:        draftService, analytics: analyticsStore, scheduling: schedulingService,
		context: background, logger: logger,
	})
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(core.ListenHost, strconv.Itoa(core.ListenPort))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	if schedulingService != nil {
		jobs.Go(func() { schedulingService.Run(background) })
	}
	jobs.Go(func() { cleanupExpired(background, db, environment, env, logger) })
	if analyticsStore != nil {
		jobs.Go(func() {
			for background.Err() == nil {
				if err := analyticsStore.Cleanup(background, time.Now()); err != nil && background.Err() == nil {
					logger.Warn("Analytics cleanup failed", "error", err)
				}
				if !waitFor(background, 24*time.Hour) {
					return
				}
			}
		})
	}
	if draftService != nil {
		jobs.Go(func() { draftService.RunCleanup(background, logger) })
	}
	if _, supplied := env.Optional("THIN_APPVIEW_RSS_FEED_POLL_ENABLED"); !supplied || env.Truthy("THIN_APPVIEW_RSS_FEED_POLL_ENABLED") {
		jobs.Go(func() { pollRSS(background, db, ingestion, env, logger) })
	}
	if ingestion.AuthorPDS != nil {
		jobs.Go(func() {
			ingestion.RunAuthorHistory(background, func(err error) {
				logger.Debug("Scoped PDS author history did not finish", "error", err)
			})
		})
	}
	if discovery != nil {
		for range 4 {
			jobs.Go(func() {
				discovery.Run(background, func(err error) {
					logger.Debug("Publication discovery did not finish", "error", err)
				})
			})
		}
	}
	if telemetry != nil {
		heartbeat := operations.NewHeartbeatJob(store, "appview", environment, ops.InstanceID, dependencyProbe(db), telemetry, logger)
		jobs.Go(func() { telemetry.Run(background) })
		jobs.Go(func() { heartbeat.Run(background) })
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	logger.Info("Starting Social Wire AppView", "environment", environment, "address", listener.Addr())
	return serve(ctx, server, listener)
}

func initialize(ctx context.Context, db *sqlitedb.DB, core config.Core, draftsEnabled bool) error {
	if err := thinstore.Migrate(ctx, db); err != nil {
		return fmt.Errorf("initialize reader schema: %w", err)
	}
	if err := publications.Migrate(ctx, db); err != nil {
		return fmt.Errorf("initialize publication schema: %w", err)
	}
	if err := reader.Migrate(ctx, db); err != nil {
		return fmt.Errorf("initialize read-state schema: %w", err)
	}
	if err := operations.Migrate(ctx, db); err != nil {
		return fmt.Errorf("initialize operations schema: %w", err)
	}
	if err := syncapi.Migrate(ctx, db); err != nil {
		return fmt.Errorf("initialize PDS cache schema: %w", err)
	}
	if core.ToolsEnabled {
		if err := analytics.Migrate(ctx, db); err != nil {
			return fmt.Errorf("initialize analytics schema: %w", err)
		}
	}
	if core.PodcastsEnabled {
		if err := podcasts.Migrate(ctx, db); err != nil {
			return fmt.Errorf("initialize podcasts schema: %w", err)
		}
	}
	if draftsEnabled {
		if err := drafts.Migrate(ctx, db); err != nil {
			return fmt.Errorf("initialize draft schema: %w", err)
		}
	}
	return nil
}

func serve(ctx context.Context, server *http.Server, listener net.Listener) error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			<-done
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func dependencyProbe(db *sqlitedb.DB) operations.DependencyProbe {
	return func(ctx context.Context) (operations.ProbeResult, error) {
		if err := db.Ping(ctx); err != nil {
			return operations.ProbeResult{}, err
		}
		now := time.Now()
		return operations.ProbeResult{
			Liveness:     operations.Healthy,
			Readiness:    operations.Healthy,
			Freshness:    operations.Unknown,
			Completeness: operations.Unknown,
			DependencyState: map[string]string{
				"appview_database":        "ready",
				"projection_freshness":    "unmeasured",
				"projection_completeness": "unknown",
			},
			RequiredDependencyKeys: map[string]struct{}{"appview_database": {}},
			ObservedAt:             now,
			ValidUntil:             now.Add(30 * time.Second),
		}, nil
	}
}
