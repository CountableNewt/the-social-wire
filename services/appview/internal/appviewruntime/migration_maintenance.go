package appviewruntime

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

func migrationMaintenanceEnabled(env config.Env) (bool, error) {
	switch strings.ToLower(env.String("MIGRATION_MAINTENANCE_MODE")) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("MIGRATION_MAINTENANCE_MODE must be true or false")
	}
}

// This branch precedes all normal configuration and dependency initialization.
// Even health checks and job invocations must remain unavailable during import.
func runMigrationMaintenance(ctx context.Context, surface Surface, args []string, env config.Env, logger *slog.Logger) error {
	if env.String("RUNTIME_MODE") != "serverless" {
		return errors.New("migration maintenance requires RUNTIME_MODE=serverless")
	}
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}
	opts := flag.NewFlagSet("migration-maintenance", flag.ContinueOnError)
	opts.SetOutput(io.Discard)
	port := opts.String("port", env.StringOr("PORT", "8080"), "HTTP port")
	host := opts.String("hostname", env.StringOr("BIND_HOST", "0.0.0.0"), "HTTP bind address")
	for _, arg := range args {
		if strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-") == "initialize-only" {
			return errors.New("--initialize-only is forbidden during migration maintenance")
		}
	}
	if err := opts.Parse(args); err != nil {
		return errors.New("invalid migration maintenance arguments; only serve, --port and --hostname are supported")
	}
	if opts.NArg() != 0 {
		return errors.New("unexpected migration maintenance argument")
	}
	number, err := strconv.Atoi(*port)
	if err != nil || number < 0 || number > 65535 {
		return errors.New("invalid migration maintenance port")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(number)))
	if err != nil {
		return err
	}
	logger.Info("Starting migration maintenance Function", "surface", surface)
	return serve(ctx, &http.Server{Handler: migrationMaintenanceHandler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}, listener)
}

func migrationMaintenanceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Service temporarily unavailable", http.StatusServiceUnavailable)
	})
}
