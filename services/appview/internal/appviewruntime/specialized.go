package appviewruntime

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/analytics"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/drafts"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/ingest"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/operations"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reader"
	scheduler "github.com/CountableNewt/the-social-wire/services/appview/internal/scheduling/backend"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/thinstore"
)

func runSpecialized(ctx context.Context, surface Surface, args []string, env config.Env, logger *slog.Logger) error {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	opts := flag.NewFlagSet(string(surface), flag.ContinueOnError)
	initializeOnly := opts.Bool("initialize-only", false, "initialize database schemas and exit")
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
	if *initializeOnly {
		if surface != Maintenance {
			return errors.New("schema initialization belongs to the maintenance surface")
		}
		return runAll(ctx, args, env, logger)
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
	mode, err := jobrunner.ParseMode(env.String("JOBS_RUNNER_MODE"))
	if err != nil {
		return err
	}
	serverless, err := serverlessRuntime(env, core, mode)
	if err != nil {
		return err
	}
	if !serverless {
		return errors.New("specialized AppView surfaces require serverless runtime and external jobs")
	}
	services, close, err := openSpecializedServices(ctx, surface, core, env, logger)
	if err != nil {
		return err
	}
	defer close()
	handler, err := buildRouter(core, env, services)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(core.ListenHost, strconv.Itoa(core.ListenPort)))
	if err != nil {
		return err
	}
	logger.Info("Starting Social Wire Function", "surface", surface, "address", listener.Addr())
	return serve(ctx, &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}, listener)
}

// Only this surface's persistent dependencies are opened. No interval runner,
// telemetry flush loop, heartbeat or history worker is started in a Function.
func openSpecializedServices(ctx context.Context, surface Surface, core config.Core, env config.Env, logger *slog.Logger) (services runtimeServices, close func(), err error) {
	services = runtimeServices{surface: surface, context: ctx, logger: logger, serverless: true, operations: operations.LoadConfiguration(env)}
	var cleanups []func()
	close = func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	defer func() {
		if err != nil {
			close()
		}
	}()
	client := publicHTTPClient()
	cleanups = append(cleanups, client.CloseIdleConnections)
	services.repo = atproto.NewRepoClient(client, core.PLCURL, logger)
	if surface.appviewDatabase() {
		err = startupStage(logger, surface, "appview-open", func() error {
			var e error
			services.db, e = openDatabase(ctx, core)
			return e
		})
		if err != nil {
			return
		}
		cleanups = append(cleanups, func() { _ = services.db.Close() })
	}
	if surface.draftStorage() {
		err = startupStage(logger, surface, "draft-storage", func() error {
			var e error
			services.drafts, e = drafts.FromEnvironment(services.db, env)
			return e
		})
		if err != nil {
			err = fmt.Errorf("configure draft storage: %w", err)
			return
		}
	}
	if services.db != nil {
		if err = startupStage(logger, surface, "appview-schema", func() error {
			return verifyRuntimeSchema(ctx, services.db, services.drafts != nil)
		}); err != nil {
			return
		}
		if services.operations.Enabled {
			environment, e := operations.RequireEnvironment(env)
			if e != nil {
				err = e
				return
			}
			services.telemetry = operations.NewTelemetryBuffer(operations.NewStore(services.db, environment, logger), logger)
		}
	}
	if surface.scheduler() {
		err = startupStage(logger, surface, "scheduling-open", func() error {
			var e error
			services.scheduling, e = scheduler.Open(ctx, core, env, client, logger)
			return e
		})
		if err != nil {
			err = fmt.Errorf("initialize scheduling: %w", err)
			return
		}
		if services.scheduling != nil {
			cleanups = append(cleanups, func() { _ = services.scheduling.Close() })
		}
	}
	var ingestion *ingest.Service
	var discovery *ingest.Microcosm
	if surface.ingestion() {
		mode := env.StringOr("PUBLIC_CONTENT_DISCOVERY_MODE", "microcosm")
		if mode != "microcosm" && mode != "off" {
			err = errors.New("PUBLIC_CONTENT_DISCOVERY_MODE must be microcosm or off")
			return
		}
		retention := configuredSeconds(env, "THIN_APPVIEW_CONTENT_TTL_SECONDS", 30*24*time.Hour)
		rssLimit, _ := strconv.Atoi(env.String("THIN_APPVIEW_MAX_RSS_ITEMS_PER_FEED"))
		content := thinstore.SQLiteIngestStore{DB: services.db}
		ingestion = &ingest.Service{Repo: services.repo, Store: content, ContentRetention: retention, MaximumRSSItems: rssLimit, HistoryDB: services.db}
		if mode == "off" {
			ingestion.AuthorPDS = services.repo.Resolver()
		} else {
			discovery = ingest.NewMicrocosm(client, content, services.db)
		}
		services.enroll = enrollment(services.db, ingestion, discovery)
		services.readerOptions = reader.Options{DurableRecovery: true, Bootstrap: bootstrapDiscovery(ctx, discovery, logger), VerifyEntry: ingest.NewEntryVerifier(services.db, core.PLCURL, retention).Verify, RefreshScope: refreshScope(ingestion, discovery)}
	}
	if surface == Analytics || surface == Maintenance {
		services.analytics = analytics.NewStore(services.db, core.GatewayAppViewInternalSecret)
	}
	if surface == Maintenance {
		environment, e := operations.RequireEnvironment(env)
		if e != nil {
			err = e
			return
		}
		services.maintenance, err = newMaintenanceRunner(services.db, env.String("JOBS_SECRET"), services.scheduling, services.drafts, discovery, maintenanceOptions{ingestion: ingestion, analytics: services.analytics, environment: environment, env: env, recovery: reader.NewRecoveryWorker(services.db, services.repo).RunOnce})
	}
	return
}
