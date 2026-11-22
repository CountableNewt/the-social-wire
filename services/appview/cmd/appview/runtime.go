package main

import (
	"errors"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	jobrunner "github.com/CountableNewt/the-social-wire/services/appview/internal/jobs"
)

func serverlessRuntime(env config.Env, core config.Core, runner jobrunner.Mode) (bool, error) {
	mode := env.StringOr("RUNTIME_MODE", "server")
	if mode != "server" && mode != "serverless" {
		return false, errors.New("RUNTIME_MODE must be server or serverless")
	}
	if env.String("VERCEL") != "" && mode != "serverless" {
		return false, errors.New("Vercel requires explicit RUNTIME_MODE=serverless")
	}
	cron := env.String("CRON_SECRET")
	if cron != "" && (!jobrunner.ValidSecret(cron) || cron == env.String("JOBS_SECRET")) {
		return false, errors.New("CRON_SECRET must be a valid dedicated secret distinct from JOBS_SECRET")
	}
	if mode == "serverless" && (core.DatabaseBackend != "turso" || runner != jobrunner.External || !jobrunner.ValidSecret(env.String("JOBS_SECRET")) || !jobrunner.ValidSecret(cron)) {
		return false, errors.New("serverless requires Turso, external jobs and a dedicated CRON_SECRET")
	}
	return mode == "serverless", nil
}
