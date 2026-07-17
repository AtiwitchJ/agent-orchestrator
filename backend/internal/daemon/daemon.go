// Package daemon owns the Modern Agent backend process: config loading,
// loopback HTTP serving, durable storage, CDC fan-out, lifecycle wiring, and
// graceful shutdown.
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modernagent/modern-agent/backend/internal/adapters/runtime/runtimeselect"
	"github.com/modernagent/modern-agent/backend/internal/config"
	"github.com/modernagent/modern-agent/backend/internal/daemon/supervisor"
	"github.com/modernagent/modern-agent/backend/internal/domain"
	"github.com/modernagent/modern-agent/backend/internal/httpd"
	"github.com/modernagent/modern-agent/backend/internal/notify"
	"github.com/modernagent/modern-agent/backend/internal/observe/heartbeat"
	"github.com/modernagent/modern-agent/backend/internal/observe/stallmon"
	"github.com/modernagent/modern-agent/backend/internal/ports"
	"github.com/modernagent/modern-agent/backend/internal/preview"
	"github.com/modernagent/modern-agent/backend/internal/runfile"
	agentsvc "github.com/modernagent/modern-agent/backend/internal/service/agent"
	companysvc "github.com/modernagent/modern-agent/backend/internal/service/company"
	importsvc "github.com/modernagent/modern-agent/backend/internal/service/importer"
	messagesvc "github.com/modernagent/modern-agent/backend/internal/service/message"
	notificationsvc "github.com/modernagent/modern-agent/backend/internal/service/notification"
	orgsvc "github.com/modernagent/modern-agent/backend/internal/service/org"
	projectsvc "github.com/modernagent/modern-agent/backend/internal/service/project"
	workboardsvc "github.com/modernagent/modern-agent/backend/internal/service/workboard"
	"github.com/modernagent/modern-agent/backend/internal/skillassets"
	"github.com/modernagent/modern-agent/backend/internal/storage/sqlite"
	"github.com/modernagent/modern-agent/backend/internal/terminal"
)

// Run starts the daemon and blocks until it exits. SIGINT/SIGTERM drive
// graceful shutdown through the HTTP server and background workers.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := newLogger()

	// Fail fast only if a daemon is genuinely still serving the recorded port.
	// CheckStale confirms the run-file's PID is alive, but that alone is not
	// proof a predecessor owns the port: the file leaks when the daemon is hard
	// killed without a graceful shutdown (the norm on Windows, where the desktop
	// supervisor can only TerminateProcess it), and Windows reuses the recorded
	// PID for unrelated processes. So a "live" PID is verified against an actual
	// /healthz probe; a run-file left by a crashed/hard-killed/reused-PID
	// predecessor is treated as stale and overwritten when the new server starts.
	if live, err := runfile.CheckStale(cfg.RunFilePath); err != nil {
		return fmt.Errorf("inspect run-file: %w", err)
	} else if live != nil && runFileOwnerServing(&http.Client{Timeout: staleProbeTimeout}, config.LoopbackHost, live) {
		return fmt.Errorf("daemon already running (pid %d, port %d); refusing to start", live.PID, live.Port)
	}

	// Open the durable store and bring up the CDC substrate: DB triggers capture
	// changes into change_log, the poller tails it, and the broadcaster fans
	// events out to live transports.
	store, err := sqlite.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = store.Close() }()

	// Refresh the embedded using-ao skill into the data dir so worker sessions
	// in any project can read the ao CLI catalog from a stable absolute path.
	// Non-fatal: the skill is an enhancement over `ao --help`, not required.
	if err := skillassets.Install(cfg.DataDir); err != nil {
		log.Warn("install using-ao skill", "err", err)
	}

	telemetrySink := newTelemetrySink(cfg, store, log)
	defer func() { _ = telemetrySink.Close(context.Background()) }()
	telemetrySink.Emit(context.Background(), ports.TelemetryEvent{
		Name:       "ao.daemon.started",
		Source:     "daemon",
		OccurredAt: time.Now().UTC(),
		Level:      ports.TelemetryLevelInfo,
		Payload: map[string]any{
			"port":  cfg.Port,
			"agent": cfg.Agent,
		},
	})

	// signal.NotifyContext cancels ctx on SIGINT/SIGTERM, which drives the
	// graceful shutdown inside Server.Run and stops the background goroutines.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cdcPipe, err := startCDC(ctx, store, log)
	if err != nil {
		return err
	}

	// Terminal streaming: the selected runtime (tmux on macOS/Linux, conpty on Windows) supplies the
	// attach Stream and liveness; the CDC broadcaster feeds the session-state channel. The manager
	// is handed to httpd, which mounts it at /mux. Raw PTY bytes never flow
	// through the CDC change_log -- only session-state events do.
	runtimeAdapter := runtimeselect.New(log)
	// outputPulse is the stall monitor's conservative secondary signal: it
	// records the last time each terminal produced real PTY output,
	// independent of the agent's own activity_state hook. See
	// terminal.OutputPulse and stallmon's conservative-only use of it.
	outputPulse := terminal.NewOutputPulse()
	termMgr := terminal.NewManager(runtimeAdapter, cdcPipe.Broadcaster, log, terminal.WithOutputPulse(outputPulse))
	defer termMgr.Close()

	// The agent messenger sends validated user input to the session's live
	// runtime pane. Keep this path small until durable inbox semantics are needed.
	// Built before the Lifecycle Manager so the LCM can use it for SCM-driven
	// agent nudges (CI failure, review feedback, merge conflict).
	messenger := newSessionMessenger(store, runtimeAdapter, log)
	notificationHub := notify.NewHub()
	notifier := notificationsvc.New(notificationsvc.Deps{Store: store})
	notificationWriter := notify.New(notify.Deps{Store: store, Publisher: notificationHub})

	// Bring up the Lifecycle Manager and the reaper first: it makes the session
	// lifecycle write path live (reducer write -> store -> DB trigger ->
	// change_log -> poller -> broadcaster) and gives startSession the shared LCM.
	lcStack := startLifecycle(ctx, store, runtimeAdapter, messenger, notificationWriter, telemetrySink, log)
	lcStack.scmDone = startSCMObserver(ctx, store, lcStack.LCM, log)
	lcStack.deliverableDone = startDeliverableObserver(ctx, store, lcStack.LCM, log)

	// Wire the controller-facing session service over the same store + LCM, the
	// selected runtime, a gitworktree workspace, the per-session agent resolver
	// (AO_AGENT validated here for compatibility), and the agent messenger, then mount it
	// on the API.
	sessionSvc, reviewSvc, sessMgr, err := startSession(cfg, runtimeAdapter, store, lcStack.LCM, messenger, telemetrySink, log)
	if err != nil {
		stop()
		lcStack.Stop()
		if cdcErr := cdcPipe.Stop(); cdcErr != nil {
			log.Error("cdc pipeline shutdown", "err", cdcErr)
		}
		return fmt.Errorf("wire session service: %w", err)
	}
	lcStack.trackerDone = startTrackerIntake(ctx, store, sessionSvc, log)
	workboardDone := startWorkboardDispatcher(ctx, store, sessionSvc, log)
	previewDone := preview.NewPoller(store, sessionSvc, "http://"+cfg.Addr(), preview.PollerConfig{Logger: log}).Start(ctx)

	// stallmon watches for worker sessions whose activity_state has claimed
	// "active" long past AO_STALL_THRESHOLD with no fresh signal, and (unless
	// AO_STALL_AUTOKILL=off) auto-terminates one confirmed stalled on two
	// consecutive ticks. sessionSvc.Kill reuses the exact same teardown path
	// as any other kill; notificationWriter records the audit trail before
	// every kill attempt.
	stallDone := stallmon.New(store, sessionSvc, notificationWriter, stallmon.Config{
		Threshold:   cfg.StallThreshold,
		AutoKill:    cfg.StallAutoKill,
		OutputPulse: outputPulse,
		Logger:      log,
	}).Start(ctx)

	// heartbeat periodically nudges a company PM or the holding CEO
	// orchestrator to wake up, run `ao org status`, and act where needed. It
	// is a hard no-op when AO_ORG_HEARTBEAT=off: heartbeatDone is a
	// pre-closed channel so the shutdown drain below never blocks on it.
	var heartbeatDone <-chan struct{}
	if cfg.OrgHeartbeat {
		heartbeatDone = heartbeat.New(store, sessionSvc, heartbeat.Config{Logger: log}).Start(ctx)
	} else {
		closed := make(chan struct{})
		close(closed)
		heartbeatDone = closed
	}

	// Hoisted so Org (below) can share it: EnsureHoldingHQ/EnsureCompanyHQ
	// register an auto-provisioned HQ repo through the same project service
	// the /api/v1/projects surface uses.
	projectSvc := projectsvc.NewWithDeps(projectsvc.Deps{Store: store, Sessions: sessionSvc, DefaultHarness: domain.AgentHarness(cfg.Agent), Telemetry: telemetrySink})
	policyEngine := newPolicyEngine(store, notificationWriter)

	srv, err := httpd.NewWithDeps(cfg, log, termMgr, httpd.APIDeps{
		Projects:           projectSvc,
		Companies:          companysvc.New(store),
		Org:                orgsvc.NewWithDeps(orgsvc.Deps{Store: store, Projects: projectSvc, DataDir: cfg.DataDir}),
		Messages:           messagesvc.New(messagesvc.Deps{Store: store}),
		Agents:             agentsvc.New(),
		Sessions:           sessionSvc,
		Reviews:            reviewSvc,
		Policy:             policyEngine,
		Notifications:      notifier,
		NotificationStream: notificationHub,
		Import:             importsvc.New(importsvc.Deps{Store: store}),
		Workboard:          workboardsvc.New(store),
		CDC:                store,
		Events:             cdcPipe.Broadcaster,
		Activity:           lcStack.LCM,
		Telemetry:          telemetrySink,
	})
	if err != nil {
		stop()
		<-workboardDone
		<-previewDone
		<-stallDone
		<-heartbeatDone
		lcStack.Stop()
		if cdcErr := cdcPipe.Stop(); cdcErr != nil {
			log.Error("cdc pipeline shutdown", "err", cdcErr)
		}
		return err
	}

	// Reconcile sessions on boot: adopt crash-surviving runtimes, capture and
	// terminate dead ones, reap leaked tmux, then restore shutdown-saved
	// sessions. Best-effort: a failure is logged but never blocks boot. Placed
	// before srv.Run so sessions are consistent before the server serves.
	if reconcileErr := sessMgr.Reconcile(ctx); reconcileErr != nil {
		log.Error("reconcile sessions on boot failed", "err", reconcileErr)
	}

	// ponytail: 5s tolerates a brief frontend restart; tune if dev hot-reload trips it.
	const supervisorGrace = 5 * time.Second

	if ln, addr, err := supervisor.Listen(cfg.RunFilePath); err != nil {
		// Non-fatal: without the link the daemon still works (e.g. headless "ao start"),
		// it just will not auto-stop when a frontend dies. Do not block startup on it.
		log.Warn("supervisor: listener unavailable; frontend-death auto-stop disabled", "err", err)
	} else {
		log.Info("supervisor: listening", "addr", addr)
		sup := supervisor.New(supervisorGrace, srv.RequestShutdown, log)
		go func() {
			if err := sup.Serve(ctx, ln); err != nil {
				log.Warn("supervisor: serve stopped with error", "err", err)
			}
		}()
	}

	runErr := srv.Run(ctx)

	// Both graceful shutdown paths (SIGTERM and POST /shutdown) funnel through
	// srv.Run returning. We deliberately do NOT tear down sessions here: they
	// survive the daemon exit and the next boot's Reconcile adopts them,
	// preserving session IDs. The narrowed sessionLifecycle interface makes
	// teardown-on-shutdown a compile error.

	// Shut the background goroutines down in order: cancel the context FIRST so
	// their loops exit, then wait for them to drain. Doing this explicitly (not
	// via defer) avoids the LIFO trap where a Stop() that blocks on ctx-cancel
	// runs before the cancel: a non-signal exit path would hang otherwise.
	stop()
	<-workboardDone
	<-previewDone
	<-stallDone
	<-heartbeatDone
	lcStack.Stop()
	if err := cdcPipe.Stop(); err != nil {
		log.Error("cdc pipeline shutdown", "err", err)
	}
	return runErr
}

// newLogger returns the daemon's slog logger. It writes to stderr so supervisors
// can capture it separately from any structured stdout protocol added later.
func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
