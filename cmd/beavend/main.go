package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/beavend/beavend/internal/config"
	"github.com/beavend/beavend/internal/snapshot"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store"
	"github.com/beavend/beavend/internal/telemetry"
)

func main() {
	var (
		roleFlag   = flag.String("role", "", "Role to run: all, runtime, or control (defaults to BEAVEND_ROLE)")
		portFlag   = flag.Int("port", 0, "HTTP port to listen on (defaults to BEAVEND_HTTP_PORT)")
		configFlag = flag.String("config", "", "Path to YAML configuration file")
	)
	flag.Parse()

	var (
		cfg *config.Config
		err error
	)
	if *configFlag != "" {
		cfg, err = config.LoadFile(*configFlag)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	if *roleFlag != "" {
		cfg.Role = *roleFlag
		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "invalid role flag: %v\n", err)
			os.Exit(1)
		}
	}

	if *portFlag != 0 {
		cfg.HTTP.Port = *portFlag
		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "invalid port flag: %v\n", err)
			os.Exit(1)
		}
	}

	logger := telemetry.SetupLogger(cfg.Log.Level, os.Stdout)
	slog.SetDefault(logger)

	slog.Info("starting beavend server",
		slog.String("role", cfg.Role),
		slog.String("env", cfg.Env),
		slog.Int("port", cfg.HTTP.Port),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tel, err := telemetry.Init(ctx, "beavend", "0.1.0")
	if err != nil {
		slog.Error("failed to initialize telemetry", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = tel.Shutdown(context.Background())
	}()

	var (
		st       *store.Store
		registry *snapshot.Registry
	)

	if cfg.Database.URL != "" {
		// Run migrations for control plane or single-node installs
		if cfg.Role == config.RoleAll || cfg.Role == config.RoleControl {
			sqlDB, err := store.OpenDB(ctx, cfg.Database.URL)
			if err != nil {
				slog.Error("failed to open database for migrations", slog.String("error", err.Error()))
			} else {
				if err := store.Migrate(ctx, sqlDB); err != nil {
					slog.Error("failed to run database migrations", slog.String("error", err.Error()))
				} else {
					slog.Info("database migrations applied successfully")
				}
				_ = sqlDB.Close()
			}
		}

		st, err = store.NewStore(ctx, cfg.Database.URL)
		if err != nil {
			slog.Warn("database connection could not be established immediately", slog.String("error", err.Error()))
		} else {
			defer st.Close()
			slog.Info("connected to persistent store pool")

			if cfg.Role == config.RoleAll || cfg.Role == config.RoleRuntime {
				registry = snapshot.NewRegistry()
				compiler, err := snapshot.NewCompiler(st.Queries())
				if err != nil {
					slog.Error("failed to initialize snapshot compiler", slog.String("error", err.Error()))
				} else {
					syncer := synckit.NewSyncer(registry, compiler, st.Queries(), nil, synckit.SyncerOptions{
						DatabaseURL: cfg.Database.URL,
						Logger:      logger,
					})
					if err := syncer.Bootstrap(ctx); err != nil {
						slog.Warn("bootstrap initial snapshots encountered warning", slog.String("error", err.Error()))
					} else {
						slog.Info("bootstrapped active project snapshots into memory")
					}
					go func() {
						if err := syncer.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
							slog.Error("syncer background task failed", slog.String("error", err.Error()))
						}
					}()
					slog.Info("snapshot syncer started with postgres listener fallback and polling backstop")
				}
			}
		}
	}

	checker := &DefaultReadyChecker{Role: cfg.Role}
	router := BuildRouter(cfg, tel, checker)
	server := NewHTTPServer(cfg, router)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening on address", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Signal handling for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		slog.Error("server encountered fatal error", slog.String("error", err.Error()))
		os.Exit(1)
	case sig := <-quit:
		slog.Info("shutting down server gracefully", slog.String("signal", sig.String()))

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shut down server cleanly", slog.String("error", err.Error()))
			os.Exit(1)
		}
		slog.Info("server exited cleanly")
	}
}
