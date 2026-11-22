package appviewruntime

import (
	"context"
	"errors"
	"log/slog"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
)

// Surface selects an independently initialized Function within the API project.
type Surface string

const (
	All          Surface = "all"
	Reader       Surface = "reader"
	Publishing   Surface = "publishing"
	Scheduling   Surface = "scheduling"
	Analytics    Surface = "analytics"
	Integrations Surface = "integrations"
	Maintenance  Surface = "maintenance"
)

func (surface Surface) valid() bool {
	switch surface {
	case All, Reader, Publishing, Scheduling, Analytics, Integrations, Maintenance:
		return true
	}
	return false
}

func (surface Surface) includes(target Surface) bool {
	return surface == "" || surface == All || surface == target
}
func (surface Surface) appviewDatabase() bool {
	return surface == All || surface == Reader || surface == Publishing || surface == Analytics || surface == Maintenance
}
func (surface Surface) draftStorage() bool {
	return surface == All || surface == Publishing || surface == Maintenance
}
func (surface Surface) scheduler() bool {
	return surface == All || surface == Scheduling || surface == Maintenance
}
func (surface Surface) ingestion() bool {
	return surface == All || surface == Reader || surface == Maintenance
}

// run preserves the standalone command's test and initialization entry point.
func run(ctx context.Context, args []string, env config.Env, logger *slog.Logger) error {
	return runSurface(ctx, All, args, env, logger)
}

func runSurface(ctx context.Context, surface Surface, args []string, env config.Env, logger *slog.Logger) error {
	if !surface.valid() {
		return errors.New("unknown AppView surface")
	}
	if enabled, err := migrationMaintenanceEnabled(env); err != nil {
		return err
	} else if enabled {
		return runMigrationMaintenance(ctx, surface, args, env, logger)
	}
	if surface == All {
		return runAll(ctx, args, env, logger)
	}
	return runSpecialized(ctx, surface, args, env, logger)
}
