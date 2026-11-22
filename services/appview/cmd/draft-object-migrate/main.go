// draft-object-migrate copies encrypted draft objects from S3 to private Blob.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/drafts"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Environ(), os.Stdout, os.Stderr))
}

func run(ctx context.Context, args, environment []string, stdout, stderr io.Writer) int {
	opts := flag.NewFlagSet("draft-object-migrate", flag.ContinueOnError)
	opts.SetOutput(io.Discard)
	receipt := opts.String("receipt", "", "absolute path to a new or resumable private receipt")
	if err := opts.Parse(args); err != nil || opts.NArg() != 0 || *receipt == "" {
		fmt.Fprintln(stderr, "Usage: draft-object-migrate --receipt /absolute/private-receipt.json")
		return 2
	}
	env := make(config.Env)
	for _, variable := range environment {
		key, value, ok := strings.Cut(variable, "=")
		if ok {
			env[key] = value
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 24*time.Hour)
	defer cancel()
	result, err := drafts.MigrateObjectsFromEnvironment(ctx, env, *receipt)
	if err != nil {
		fmt.Fprintln(stderr, drafts.ErrObjectMigration.Error())
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}
