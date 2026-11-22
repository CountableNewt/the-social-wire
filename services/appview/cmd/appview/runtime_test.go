package main

import (
	"strings"
	"testing"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
)

func TestRuntimeModeRejectsEphemeralOrUnscheduledVercelStartup(t *testing.T) {
	base := config.Env{"VERCEL": "1", "RUNTIME_MODE": "serverless", "JOBS_SECRET": strings.Repeat("j", 32), "CRON_SECRET": strings.Repeat("c", 32)}
	core := config.Core{DatabaseBackend: "turso"}
	if enabled, err := serverlessRuntime(base, core, jobrunner.External); err != nil || !enabled {
		t.Fatalf("valid serverless runtime: %t %v", enabled, err)
	}
	for _, change := range []config.Env{{"RUNTIME_MODE": "server"}, {"RUNTIME_MODE": ""}, {"RUNTIME_MODE": "unknown"}, {"CRON_SECRET": ""}, {"CRON_SECRET": strings.Repeat("j", 32)}, {"JOBS_SECRET": ""}} {
		env := config.Env{}
		for key, value := range base {
			env[key] = value
		}
		for key, value := range change {
			env[key] = value
		}
		if _, err := serverlessRuntime(env, core, jobrunner.External); err == nil {
			t.Fatalf("accepted unsafe runtime change: %v", change)
		}
	}
	if _, err := serverlessRuntime(base, config.Core{DatabaseBackend: "sqlite"}, jobrunner.External); err == nil {
		t.Fatal("serverless accepted SQLite")
	}
	if _, err := serverlessRuntime(base, core, jobrunner.Internal); err == nil {
		t.Fatal("serverless accepted internal timers")
	}
	if enabled, err := serverlessRuntime(config.Env{}, config.Core{DatabaseBackend: "sqlite"}, jobrunner.Internal); err != nil || enabled {
		t.Fatalf("default server changed: %t %v", enabled, err)
	}
}
