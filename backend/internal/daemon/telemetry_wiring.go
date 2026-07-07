package daemon

import (
	"log/slog"

	telemetryadapter "github.com/modernagent/modern-agent/backend/internal/adapters/telemetry"
	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
)

func newTelemetrySink(cfg config.Config, store *sqlite.Store, log *slog.Logger) ports.EventSink {
	if !cfg.Telemetry.Events {
		return telemetryadapter.NoopSink{}
	}
	local := telemetryadapter.NewLocalSQLiteSink(store, log)
	if cfg.Telemetry.Remote != config.TelemetryRemotePostHog {
		return local
	}
	remote, err := telemetryadapter.NewPostHogSink(cfg.DataDir, cfg.Telemetry.PostHogKey, cfg.Telemetry.PostHogHost, nil, log)
	if err != nil {
		log.Warn("telemetry remote sink disabled", "remote", cfg.Telemetry.Remote, "error", err)
		return local
	}
	return telemetryadapter.NewFanoutSink(local, remote)
}
