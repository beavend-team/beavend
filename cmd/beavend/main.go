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

	"github.com/beavend/beavend/dashboard"
	"github.com/beavend/beavend/internal/cache"
	"github.com/beavend/beavend/internal/config"
	"github.com/beavend/beavend/internal/control"
	"github.com/beavend/beavend/internal/control/audit"
	"github.com/beavend/beavend/internal/control/auth"
	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/identity"
	"github.com/beavend/beavend/internal/limits"
	"github.com/beavend/beavend/internal/memory"
	"github.com/beavend/beavend/internal/policy"
	"github.com/beavend/beavend/internal/providers/litellm"
	"github.com/beavend/beavend/internal/routing"
	"github.com/beavend/beavend/internal/runtime"
	"github.com/beavend/beavend/internal/runtime/agent"
	"github.com/beavend/beavend/internal/runtime/stages"
	"github.com/beavend/beavend/internal/snapshot"
	synckit "github.com/beavend/beavend/internal/snapshot/sync"
	"github.com/beavend/beavend/internal/store"
	"github.com/beavend/beavend/internal/store/db"
	"github.com/beavend/beavend/internal/telemetry"
	"github.com/beavend/beavend/internal/tools"
	"github.com/beavend/beavend/internal/usage"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "beavend: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
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
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if *roleFlag != "" {
		cfg.Role = *roleFlag
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid role flag: %w", err)
		}
	}

	if *portFlag != 0 {
		cfg.HTTP.Port = *portFlag
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid port flag: %w", err)
		}
	}

	logger := telemetry.SetupLogger(cfg.Log.Level, os.Stdout)
	slog.SetDefault(logger)

	slog.Info("starting beavend server",
		slog.String("role", cfg.Role),
		slog.String("env", cfg.Env),
		slog.Int("port", cfg.HTTP.Port),
	)

	tel, err := telemetry.Init(context.Background(), "beavend", "0.1.0")
	if err != nil {
		return fmt.Errorf("initialize telemetry: %w", err)
	}
	defer func() {
		_ = tel.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		st          *store.Store
		registry    *snapshot.Registry
		batchWriter *usage.BatchWriter
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

			// Start periodic rollup aggregator for control plane or combined node
			if cfg.Role == config.RoleAll || cfg.Role == config.RoleControl {
				rollupStore := usage.NewPostgresRollupStore(st.Queries())
				rollupAgg := usage.NewRollupAggregator(rollupStore, usage.RollupAggregatorConfig{
					Interval: cfg.Usage.RollupInterval,
				})
				go rollupAgg.Start(ctx)
				slog.Info("usage rollup aggregator background service started", slog.Duration("interval", cfg.Usage.RollupInterval))
			}
		}
	}

	var (
		cacheStore cache.PurgeableCacheStore
	)
	if cfg.Redis.URL != "" {
		if rOpts, err := redis.ParseURL(cfg.Redis.URL); err == nil {
			redisClient := redis.NewClient(rOpts)
			cacheStore = cache.NewRedisCacheStore(redisClient)
		}
	}
	if cacheStore == nil {
		cacheStore = cache.NewMemoryCacheStore()
	}

	var (
		memoryProvider core.MemoryProvider
		threadStore    core.ThreadStore
	)
	if st != nil && st.Queries() != nil {
		memoryProvider = memory.NewPGVectorMemoryProvider(st.Queries())
		threadStore = memory.NewPostgresThreadStore(st.Queries())
	} else {
		memoryProvider = memory.NewLocalMemoryProvider()
		threadStore = memory.NewMemoryThreadStore()
	}
	if cfg.Redis.URL != "" {
		if rOpts, err := redis.ParseURL(cfg.Redis.URL); err == nil {
			redisClient := redis.NewClient(rOpts)
			threadStore = memory.NewRedisThreadStore(redisClient)
		}
	}
	memEngine := memory.NewEngine(memory.EngineConfig{
		DefaultProvider: memoryProvider,
		ThreadStore:     threadStore,
	})

	var routerOpts RouterOptions
	if cfg.Role == config.RoleAll || cfg.Role == config.RoleRuntime {
		if registry == nil {
			registry = snapshot.NewRegistry()
		}

		modelResolver := routing.NewResolver()
		litellmAdapter, err := litellm.NewAdapter(litellm.Config{
			BaseURL:   cfg.LiteLLM.URL,
			MasterKey: cfg.LiteLLM.Key,
		})
		if err == nil {
			modelResolver.RegisterAdapter("litellm", litellmAdapter)
		} else {
			slog.Warn("failed to initialize litellm adapter", slog.String("error", err.Error()))
		}

		var continuationStore tools.SuspendedStateStore
		if cfg.Redis.URL != "" {
			if rOpts, err := redis.ParseURL(cfg.Redis.URL); err == nil {
				redisClient := redis.NewClient(rOpts)
				continuationStore = tools.NewRedisSuspendedStateStore(redisClient)
			}
		}
		if continuationStore == nil {
			continuationStore = tools.NewInMemorySuspendedStateStore()
		}
		continuationMgr := tools.NewContinuationManager(continuationStore, []byte(cfg.Security.MasterKey))

		authenticator := tools.NewDownstreamAuthenticator(nil)
		isDev := cfg.Env == "development" || cfg.Env == "test"
		execRegistry := agent.NewDefaultExecutorRegistry(authenticator, isDev)
		policyCompiler, _ := policy.NewCompiler()
		policyEvaluator, _ := policy.NewEvaluator(policyCompiler)

		loop := agent.NewLoop(agent.LoopConfig{
			PolicyEvaluator:  policyEvaluator,
			ContinuationMgr:  continuationMgr,
			ExecutorRegistry: execRegistry,
			CacheStore:       cacheStore,
		})

		limitStore := limits.NewMemoryLimitStore(nil)
		authzStage, _ := stages.NewAuthZStage(nil)
		limitsStage, _ := stages.NewLimitsStage(limitStore, nil, nil)
		inputGuardStage := stages.NewInputGuardrailStage()
		cacheStage := stages.NewCacheStage(cacheStore)
		contextStage := stages.NewContextStage(memEngine, threadStore)
		promptStage, _ := stages.NewPromptStage(nil)
		routingStage, _ := stages.NewModelRoutingStage(modelResolver)
		execStage, _ := stages.NewExecutionStage(modelResolver, loop)
		outputGuardStage := stages.NewOutputGuardrailStage()

		// Setup usage tracking buffer, batch writer, and tracker
		usageBuf := usage.NewBuffer(usage.BufferConfig{
			Capacity: cfg.Usage.BufferSize,
			Policy:   usage.DropOnFull,
		})

		var usageSink usage.Sink
		if st != nil {
			usageSink = usage.NewPostgresSink(st.Pool(), st.Queries())
		} else {
			usageSink = usage.NewInMemorySink()
		}

		batchWriter = usage.NewBatchWriter(usageSink, usageBuf, usage.BatchWriterConfig{
			BatchSize:     cfg.Usage.BatchSize,
			FlushInterval: cfg.Usage.FlushInterval,
		})
		batchWriter.Start()

		usageTracker := usage.NewTracker(usageBuf, limitStore)

		pipeline := runtime.NewContextAwarePipeline(authzStage, limitsStage, inputGuardStage, cacheStage, contextStage, promptStage, routingStage, execStage, outputGuardStage)
		runtimeHandler := runtime.NewHandler(registry, pipeline, runtime.HandlerOptions{
			DefaultEnvironment: cfg.Env,
			ContinuationMgr:    continuationMgr,
			ExecutorRegistry:   execRegistry,
			UsageStore:         usageTracker,
			CacheStore:         cacheStore,
			ThreadStore:        threadStore,
			MemoryEngine:       memEngine,
		})
		routerOpts.RuntimeHandler = runtimeHandler

		verifier := identity.NewVerifier(nil)
		authMw := identity.AuthMiddleware(registry, verifier, identity.WithDefaultEnvironment(cfg.Env))
		routerOpts.AuthMiddleware = authMw
	}

	if cfg.Role == config.RoleAll || cfg.Role == config.RoleControl {
		// Admin token manager (uses MasterKey for signing session tokens and cookies)
		adminTokenMgr := auth.NewTokenManager([]byte(cfg.Security.MasterKey), 24*time.Hour)
		routerOpts.AdminTokenManager = adminTokenMgr

		var (
			ctrlAuditor audit.Auditor
			ctrlQueries *db.Queries
			publisher   synckit.Publisher
		)

		if st != nil {
			ctrlQueries = st.Queries()
			ctrlAuditor = audit.NewStoreAuditor(ctrlQueries)

			// Seed default admin user if not already present
			if err := auth.SeedDefaultAdmin(ctx, ctrlQueries, cfg.Admin.Email, cfg.Admin.Password); err != nil {
				slog.Warn("seed default admin user encountered warning", slog.String("error", err.Error()))
			}

			// Publisher for snapshot invalidation: Redis + Postgres NOTIFY fallback
			var publishers []synckit.Publisher
			if cfg.Redis.URL != "" {
				if rOpts, err := redis.ParseURL(cfg.Redis.URL); err == nil {
					redisClient := redis.NewClient(rOpts)
					publishers = append(publishers, synckit.NewRedisPublisher(redisClient))
				}
			}
			if st.Pool() != nil {
				publishers = append(publishers, synckit.NewPostgresPublisher(st.Pool()))
			}
			if len(publishers) > 0 {
				publisher = synckit.NewMultiPublisher(publishers...)
			}
		} else {
			ctrlAuditor = audit.NewInMemoryAuditor()
		}

		ctrlService := control.NewService(ctrlQueries, publisher, ctrlAuditor, []byte(cfg.Security.MasterKey))
		ctrlService.SetCacheStore(cacheStore)
		ctrlService.SetMemoryProvider(memoryProvider)
		ctrlService.SetThreadStore(threadStore)
		ctrlHandler := control.NewHandler(ctrlService)
		routerOpts.ControlHandler = ctrlHandler

		secureCookie := cfg.Env == config.EnvProduction
		adminAuthHandler := auth.NewHandler(ctrlQueries, adminTokenMgr, ctrlAuditor, secureCookie)
		routerOpts.AdminAuthHandler = adminAuthHandler

		// Embedded Dashboard FS
		dashFS, err := dashboard.FS()
		if err != nil {
			slog.Warn("failed to load embedded dashboard filesystem", slog.String("error", err.Error()))
		} else {
			routerOpts.DashboardFS = dashFS
		}
	}

	checker := &DefaultReadyChecker{Role: cfg.Role}
	router := BuildRouter(cfg, tel, checker, routerOpts)
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
		return fmt.Errorf("server fatal error: %w", err)
	case sig := <-quit:
		slog.Info("shutting down server gracefully", slog.String("signal", sig.String()))
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shut down server cleanly", slog.String("error", err.Error()))
		}

		if batchWriter != nil {
			if err := batchWriter.Close(shutdownCtx); err != nil {
				slog.Warn("batch writer shutdown encountered error", slog.String("error", err.Error()))
			} else {
				slog.Info("usage batch writer flushed all pending events cleanly")
			}
		}
		slog.Info("server exited cleanly")
		return nil
	}
}
