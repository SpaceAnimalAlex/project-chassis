// Command chassis is the single static binary entrypoint for Project
// Chassis: it opens the embedded SQLite database, wires the domain
// Implements, starts the optional mail sync worker, and serves the HTTP
// triage UI. Everything needed to run lives in this one executable — no
// external services, no node_modules, no JVM.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/db"
	"github.com/project-chassis/chassis/internal/implement"
	"github.com/project-chassis/chassis/internal/mailengine"
	"github.com/project-chassis/chassis/internal/mailengine/graph"
	"github.com/project-chassis/chassis/internal/presence"
	"github.com/project-chassis/chassis/internal/web"
	"github.com/project-chassis/chassis/internal/web/stream"

	// Blank-imported so each Implement's init() self-registers into
	// implement.DefaultRegistry — main never needs to know their concrete types.
	_ "github.com/project-chassis/chassis/internal/implement/civic"
	_ "github.com/project-chassis/chassis/internal/implement/edu"
	_ "github.com/project-chassis/chassis/internal/implement/it"
	_ "github.com/project-chassis/chassis/internal/implement/mro"
)

// sessionSweepInterval controls how often expired sessions are purged from
// the database. ValidateSession already self-cleans on the read path, so
// this is just housekeeping for sessions nobody ever tries to use again
// (e.g. an operator who left and never came back).
const sessionSweepInterval = 1 * time.Hour

func main() {
	var (
		dbPath  = flag.String("db", envOr("CHASSIS_DB_PATH", "chassis.db"), "Path to the SQLite database file")
		addr    = flag.String("addr", envOr("CHASSIS_ADDR", ":8080"), "HTTP listen address")
		verbose = flag.Bool("verbose", os.Getenv("CHASSIS_VERBOSE") == "1", "Enable debug logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, *dbPath, *addr); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("chassis exited with error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, dbPath, addr string) error {
	database, err := db.Open(db.Config{Path: dbPath})
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		return err
	}
	logger.Info("database ready", "path", dbPath)

	repo := db.NewRepository(database)

	if err := bootstrapAdmin(ctx, repo, logger); err != nil {
		return err
	}

	tracker := presence.NewTracker()
	sweepStop := make(chan struct{})
	go tracker.Run(sweepStop)
	defer close(sweepStop)

	go runSessionSweep(ctx, repo, logger)

	// One hub, shared by the HTTP layer (broadcasts staff replies, serves
	// the SSE stream) and the mail sync worker (broadcasts inbound email) —
	// see internal/web/stream for why a live viewer needs to hear from both.
	hub := stream.NewHub()

	prefix := envOr("CHASSIS_ITEM_PREFIX", "HD")
	threader := mailengine.NewThreader(database)
	ingestor := mailengine.NewIngestor(repo, threader, hub, prefix)

	handler, err := web.NewRouter(repo, repo, repo, ingestor, implement.DefaultRegistry, tracker, hub, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Mail sync is opt-in: without Microsoft Graph credentials configured,
	// Chassis runs perfectly well as a local-only triage queue (the shop
	// floor keeps working even if nobody's wired up email yet).
	if worker, ok := buildGraphWorker(repo, hub, logger); ok {
		workerCtx, cancelWorker := context.WithCancel(ctx)
		defer cancelWorker()
		go worker.Start(workerCtx)
	} else {
		logger.Info("mail sync disabled (CHASSIS_GRAPH_* env vars not fully set)")
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("chassis listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-serverErr:
		return err
	}
}

// bootstrapAdmin creates the first admin account from environment variables
// when the users table is empty — there's no other way to get a working
// login on a brand-new install, since the sole path to a password is
// bcrypt-hashing one through this same repository.
func bootstrapAdmin(ctx context.Context, repo *db.Repository, logger *slog.Logger) error {
	var count int
	if err := repo.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	email := os.Getenv("CHASSIS_BOOTSTRAP_ADMIN_EMAIL")
	password := os.Getenv("CHASSIS_BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || password == "" {
		logger.Warn("no users exist and CHASSIS_BOOTSTRAP_ADMIN_EMAIL/CHASSIS_BOOTSTRAP_ADMIN_PASSWORD are not set — nobody can log in yet")
		return nil
	}

	fullName := envOr("CHASSIS_BOOTSTRAP_ADMIN_NAME", "Administrator")
	res, err := repo.DB().ExecContext(ctx, `
		INSERT INTO users (uuid, email, full_name, role, is_active)
		VALUES (?, ?, ?, ?, 1);
	`, uuid.NewString(), email, fullName, core.RoleAdmin)
	if err != nil {
		return err
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := repo.SetPassword(ctx, userID, password); err != nil {
		return err
	}

	logger.Info("bootstrap admin account created", "email", email)
	return nil
}

// runSessionSweep periodically deletes expired sessions until ctx is cancelled.
func runSessionSweep(ctx context.Context, repo *db.Repository, logger *slog.Logger) {
	ticker := time.NewTicker(sessionSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := repo.PruneExpiredSessions(ctx)
			if err != nil {
				logger.Error("session sweep failed", "error", err)
				continue
			}
			if n > 0 {
				logger.Info("pruned expired sessions", "count", n)
			}
		}
	}
}

func buildGraphWorker(repo *db.Repository, hub *stream.Hub, logger *slog.Logger) (*mailengine.SyncWorker, bool) {
	cfg := graph.Config{
		TenantID:     os.Getenv("CHASSIS_GRAPH_TENANT_ID"),
		ClientID:     os.Getenv("CHASSIS_GRAPH_CLIENT_ID"),
		ClientSecret: os.Getenv("CHASSIS_GRAPH_CLIENT_SECRET"),
		MailboxEmail: os.Getenv("CHASSIS_GRAPH_MAILBOX"),
	}
	if cfg.TenantID == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.MailboxEmail == "" {
		return nil, false
	}

	provider := graph.NewProvider(cfg)
	if err := provider.Initialize(context.Background(), nil); err != nil {
		logger.Error("failed to initialize Microsoft Graph provider; mail sync disabled", "error", err)
		return nil, false
	}

	worker := mailengine.NewSyncWorker(mailengine.WorkerConfig{
		Provider:      provider,
		Repository:    repo,
		Notifier:      hub,
		DefaultPrefix: envOr("CHASSIS_ITEM_PREFIX", "HD"),
		Logger:        logger,
	})
	return worker, true
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
