// database-preflight inspects an offline SQLite snapshot before remote import.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/preflight"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	opts := flag.NewFlagSet("database-preflight", flag.ContinueOnError)
	opts.SetOutput(io.Discard)
	path := opts.String("snapshot", "", "absolute path to an existing offline snapshot")
	kind := opts.String("kind", "", "appview or scheduling")
	if err := opts.Parse(args); err != nil || opts.NArg() != 0 || *path == "" || (*kind != "appview" && *kind != "scheduling") {
		fmt.Fprintln(stderr, "Usage: database-preflight --kind appview|scheduling --snapshot /absolute/verified-backup.sqlite")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report, err := preflight.Inspect(ctx, *path, *kind)
	if err != nil {
		// Inspect returns fixed diagnostics; paths, SQL errors and stored values
		// never reach this output. JSON is emitted only after every check passes.
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return 1
	}
	return 0
}
