// Package main - CloudAI Fusion API Server
// The central control plane for the CloudAI Fusion platform.
// Provides RESTful APIs for cluster management, workload scheduling,
// security policy enforcement, and multi-cloud resource orchestration.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/controlplane"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/election"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/logging"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/mesh"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/messaging"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/metrics"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/migrate"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/monitor"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/resilience"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/rpcserver"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tracing"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/websocket"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/controller"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cloudai-apiserver",
		Short: "CloudAI Fusion API Server",
		Long: `CloudAI Fusion API Server - The central control plane for
cloud-native AI unified management. Provides RESTful APIs for
cluster management, workload scheduling, security enforcement,
and multi-cloud resource orchestration.`,
		RunE: runServer,
	}

	rootCmd.Flags().String("config", "", "config file path")
	rootCmd.Flags().String("host", "0.0.0.0", "server listen host")
	rootCmd.Flags().Int("port", 8080, "server listen port")
	rootCmd.Flags().String("log-level", "info", "log level (debug, info, warn, error)")
	rootCmd.Flags().Int("metrics-port", 9100, "prometheus metrics port")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("CloudAI Fusion API Server\n")
			fmt.Printf("  Version:    %s\n", Version)
			fmt.Printf("  Git Commit: %s\n", GitCommit)
			fmt.Printf("  Build Time: %s\n", BuildTime)
		},
	}
	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load(cmd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize structured logger with sampling (Problem #8.5 enhanced)
	appLogger := logging.New(logging.Config{
		Level:          cfg.LogLevel,
		Format:         "json",
		Component:      "apiserver",
		EnableSampling: true,
		SamplerConfig: &logging.SamplerConfig{
			InitialCount:   100,
			ThereafterRate: 100,
			WindowDuration: 1 * time.Minute,
		},
	})
	logger := appLogger.Logrus()
	logger.WithFields(logrus.Fields{
		"version":    Version,
		"git_commit": GitCommit,
	}).Info("Starting CloudAI Fusion API Server")

	// Graceful shutdown context using signal.NotifyContext (Problem #8.2)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize OpenTelemetry tracing (enhanced: adaptive sampling + profiling)
	tracingProvider, err := tracing.Init(ctx, tracing.Config{
		ServiceName:      "cloudai-apiserver",
		ServiceVersion:   Version,
		Endpoint:         cfg.JaegerEndpoint,
		SampleRate:       0.1,
		Enabled:          cfg.JaegerEndpoint != "",
		AdaptiveSampling: true,
		MinSampleRate:    0.01,
		MaxSampleRate:    1.0,
		TargetSpansPerSec: 200,
		Environment:      cfg.Env,
	})
	if err != nil {
		logger.WithError(err).Warn("Failed to init tracing, continuing without")
	}
	if tracingProvider != nil {
		defer func() {
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutCancel()
			if err := tracingProvider.Shutdown(shutCtx); err != nil {
				logger.WithError(err).Error("Tracing provider shutdown error")
			}
		}()
		logger.Info("OpenTelemetry tracing initialized")
	}

	// Initialize authentication service
	authService, err := auth.NewService(auth.Config{
		JWTSecret: cfg.JWTSecret,
		JWTExpiry: cfg.JWTExpiry,
	})
	if err != nil {
		return fmt.Errorf("failed to init auth service: %w", err)
	}
	logger.Info("Authentication service initialized")

	// Initialize database store (optional - graceful degradation if DB unavailable)
	dbStore, err := store.New(store.Config{
		DSN:          cfg.DatabaseDSN(),
		MaxOpenConns: cfg.DBMaxOpenConns,
		MaxIdleConns: cfg.DBMaxIdleConns,
		LogLevel:     "warn",
	})
	if err != nil {
		logger.WithError(err).Warn("Database unavailable - running without persistence (login/register disabled)")
	} else {
		authService.SetStore(dbStore)
		defer dbStore.Close()
		logger.Info("Database store initialized")

		// Run database migrations (Problem #8.1)
		sqlDB, dbErr := dbStore.DB().DB()
		if dbErr == nil {
			migrator := migrate.New(migrate.Config{DB: sqlDB, Logger: logger}, migrate.BuiltinMigrations())
			migrateCtx, migrateCancel := context.WithTimeout(ctx, 60*time.Second)
			applied, migErr := migrator.Up(migrateCtx)
			migrateCancel()
			if migErr != nil {
				logger.WithError(migErr).Warn("Database migration failed (tables may already exist via AutoMigrate)")
			} else if applied > 0 {
				logger.WithField("applied", applied).Info("Database migrations applied")
			}
		}
	}

	// Initialize feature flags (Problem #8.7)
	featureFlags := feature.NewManager(feature.Config{Logger: logger})
	logger.Info("Feature flags initialized")

	// Initialize cloud provider manager
	cloudManager, err := cloud.NewManager(cloud.ManagerConfig{
		Providers: cfg.CloudProviders,
	})
	if err != nil {
		return fmt.Errorf("failed to init cloud manager: %w", err)
	}
	logger.Info("Cloud provider manager initialized")

	// Initialize cluster manager (with DB persistence if available)
	clusterManager, err := cluster.NewManager(cluster.ManagerConfig{
		DatabaseURL:  cfg.DatabaseURL(),
		CloudManager: cloudManager,
		Store:        dbStore,
	})
	if err != nil {
		return fmt.Errorf("failed to init cluster manager: %w", err)
	}
	logger.Info("Cluster manager initialized")

	// Initialize security manager (with DB persistence if available)
	securityManager, err := security.NewManager(security.ManagerConfig{
		DatabaseURL:    cfg.DatabaseURL(),
		ClusterManager: clusterManager,
	})
	if err != nil {
		return fmt.Errorf("failed to init security manager: %w", err)
	}
	if dbStore != nil {
		securityManager.SetStore(dbStore)
	}
	logger.Info("Security manager initialized")

	// Initialize monitoring service
	monitorService, err := monitor.NewService(monitor.ServiceConfig{
		PrometheusEndpoint: cfg.PrometheusEndpoint,
		JaegerEndpoint:     cfg.JaegerEndpoint,
		MetricsPort:        cfg.MetricsPort,
	})
	if err != nil {
		return fmt.Errorf("failed to init monitoring service: %w", err)
	}
	if dbStore != nil {
		monitorService.SetStore(dbStore)
	}
	monitorService.Start(ctx)
	logger.Info("Monitoring service initialized")

	// Initialize workload manager
	workloadManager, err := workload.NewManager(workload.ManagerConfig{
		Store:  dbStore,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("failed to init workload manager: %w", err)
	}
	logger.Info("Workload manager initialized")

	// Initialize service mesh manager (eBPF/Cilium/Istio Ambient)
	meshManager, err := mesh.NewManager(mesh.Config{
		Mode:        mesh.MeshModeAmbient,
		EnableMTLS:  true,
		EnableTracing: true,
		TraceSampleRate: 0.1,
	})
	if err != nil {
		return fmt.Errorf("failed to init mesh manager: %w", err)
	}
	if dbStore != nil {
		meshManager.SetStore(dbStore)
	}
	logger.Info("Service mesh manager initialized (Istio Ambient mode)")

	// Initialize WebAssembly runtime manager
	wasmManager, err := wasm.NewManager(wasm.Config{
		DefaultRuntime: wasm.RuntimeSpin,
		MaxInstances:   1000,
		MemoryLimitMB:  128,
	})
	if err != nil {
		return fmt.Errorf("failed to init wasm manager: %w", err)
	}
	if dbStore != nil {
		wasmManager.SetStore(dbStore)
	}
	logger.Info("WebAssembly runtime manager initialized")

	// Initialize edge-cloud manager
	edgeManager, err := edge.NewManager(edge.Config{
		CloudEndpoint:     fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port),
		MaxEdgePowerWatts: 200,
		EnableAutoFailover: true,
	})
	if err != nil {
		return fmt.Errorf("failed to init edge manager: %w", err)
	}
	if dbStore != nil {
		edgeManager.SetStore(dbStore)
	}
	logger.Info("Edge-cloud manager initialized")

	// Initialize WebSocket hub (Problem #8.10)
	wsHub := websocket.NewHub(logger)
	go wsHub.Run(ctx)
	logger.Info("WebSocket hub initialized")

	// ================================================================
	// Event-Driven Architecture: Initialize EventBus & Messaging
	// ================================================================

	// Initialize Event Bus (NATS-backed with in-memory fallback)
	eventBusCfg := eventbus.Config{
		Backend:    "nats",
		NATSURL:    cfg.NATSURL,
		BufferSize: 4096,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
	}
	bus := eventbus.New(eventBusCfg, logger)
	defer bus.Close()
	logger.Info("Event bus initialized")

	// Initialize async messaging queue (NATS/Kafka)
	msgCfg := messaging.Config{
		Backend:      "nats",
		NATSURL:      cfg.NATSURL,
		KafkaBrokers: cfg.KafkaBrokers,
		KafkaGroupID: cfg.KafkaGroupID,
		MaxRetries:   3,
		RetryDelay:   5 * time.Second,
	}
	msgProducer := messaging.NewProducer(msgCfg, logger)
	defer msgProducer.Close()
	_ = msgProducer // Will be used by API handlers for async command dispatch
	logger.Info("Message producer initialized")

	// ================================================================
	// Control Plane: Independent reconciliation service (decoupled)
	// ================================================================

	// Leader Election config for HA deployments
	leaderElectionCfg := &election.Config{
		Backend:             "memory", // Use "kubernetes" in production
		LeaseDuration:       15 * time.Second,
		RenewDeadline:       10 * time.Second,
		RetryPeriod:         2 * time.Second,
		LockName:            "cloudai-fusion-apiserver",
		SplitBrainDetection: true,
		Logger:              logger,
	}

	ctrlPlane := controlplane.New(controlplane.Config{
		Logger:                  logger,
		EventBus:                bus,
		MaxConcurrentReconciles: 2,
		SyncPeriod:              10 * time.Minute,
		LeaderElection:          true,
		LeaderElectionConfig:    leaderElectionCfg,
	})
	ctrlManager := ctrlPlane.ControllerManager()

	// Register event-driven triggers: events → controller reconciliation
	ctrlPlane.RegisterEventTrigger(eventbus.TopicClusterCreated, "cluster-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicClusterUpdated, "cluster-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicClusterHealth, "cluster-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicWorkloadCreated, "workload-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicWorkloadScheduled, "workload-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicSecurityPolicyApplied, "security-policy-controller")
	ctrlPlane.RegisterEventTrigger(eventbus.TopicSecurityViolation, "security-policy-controller")

	// Register Cluster Reconciler
	clusterReconciler := controller.NewClusterReconciler(controller.ClusterReconcilerConfig{
		ClusterService: clusterManager,
		Manager:        ctrlManager,
		Logger:         logger,
	})
	if err := ctrlManager.RegisterReconciler(clusterReconciler); err != nil {
		logger.WithError(err).Error("Failed to register cluster reconciler")
	}

	// Register Workload Reconciler
	workloadReconciler := controller.NewWorkloadReconciler(controller.WorkloadReconcilerConfig{
		WorkloadService: workloadManager,
		Store:           dbStore,
		Manager:         ctrlManager,
		Logger:          logger,
	})
	if err := ctrlManager.RegisterReconciler(workloadReconciler); err != nil {
		logger.WithError(err).Error("Failed to register workload reconciler")
	}

	// Register SecurityPolicy Reconciler
	securityReconciler := controller.NewSecurityPolicyReconciler(controller.SecurityPolicyReconcilerConfig{
		SecurityService: securityManager,
		Manager:         ctrlManager,
		Logger:          logger,
	})
	if err := ctrlManager.RegisterReconciler(securityReconciler); err != nil {
		logger.WithError(err).Error("Failed to register security policy reconciler")
	}

	// Start control plane (event-driven controller manager)
	if err := ctrlPlane.Start(ctx); err != nil {
		logger.WithError(err).Error("Control plane failed to start")
	}
	logger.Info("Control plane started (event-driven: cluster, workload, security-policy reconcilers)")

	// ================================================================
	// High-Availability: Health Check Manager
	// ================================================================
	healthMgr := resilience.NewHealthManager(resilience.HealthConfig{
		CheckInterval: 10 * time.Second,
		CheckTimeout:  5 * time.Second,
		Logger:        logger,
		Version:       Version,
		Port:          cfg.MetricsPort + 2, // metrics_port+2 for health HTTP
	})

	// Register liveness checks
	healthMgr.RegisterLiveness("controller-manager", resilience.CustomChecker("controller-manager", func(ctx context.Context) error {
		if !ctrlManager.Healthy() {
			return fmt.Errorf("controller manager unhealthy")
		}
		return nil
	}))

	// Register readiness checks
	if dbStore != nil {
		healthMgr.RegisterReadiness("database", resilience.DatabaseHealthChecker(func(ctx context.Context) error {
			sqlDB, err := dbStore.DB().DB()
			if err != nil {
				return err
			}
			return sqlDB.PingContext(ctx)
		}))
	}
	healthMgr.RegisterReadiness("event-bus", resilience.CustomChecker("event-bus", func(ctx context.Context) error {
		stats := bus.Stats()
		if stats.ActiveSubscriptions == 0 && len(ctrlPlane.Status().ControllerStatus.Controllers) > 0 {
			return fmt.Errorf("event bus has no subscribers")
		}
		return nil
	}))

	// Start health check manager
	healthMgr.Start(ctx)
	healthMgr.ServeHTTP(ctx)
	healthMgr.MarkStartupComplete()
	logger.WithField("port", cfg.MetricsPort+2).Info("Health check manager started")

	// ================================================================
	// High-Availability: Multi-Level Circuit Breaker
	// ================================================================
	mlBreaker := resilience.NewMultiLevelBreaker(resilience.DefaultMultiLevelConfig())
	_ = mlBreaker // Available for API handlers and service clients
	logger.Info("Multi-level circuit breaker initialized")

	// ================================================================
	// Data Consistency: Write-Ahead Log (WAL)
	// ================================================================
	wal := store.NewWAL(store.DefaultWALConfig())
	_ = wal // Available for WAL-protected store operations
	logger.Info("Write-Ahead Log (WAL) initialized")

	// ================================================================
	// Observability: SLO Tracker & Resource Collector
	// ================================================================

	// Initialize SLO Tracker (in-process SLI/SLO evaluation)
	sloTracker := metrics.NewSLOTracker(metrics.SLOTrackerConfig{
		Definitions: metrics.DefaultSLOs(),
		Logger:      logger,
		Interval:    30 * time.Second,
	})
	sloTracker.Start(ctx)
	logger.Info("SLO tracker started")

	// Initialize resource utilization collector
	resCollector := metrics.NewResourceCollector(metrics.ResourceCollectorConfig{
		CollectInterval: 15 * time.Second,
		Logger:          logger,
		NodeName:        cfg.Host,
		EnableGoRuntime: true,
	})
	resCollector.Start(ctx)
	logger.Info("Resource utilization collector started")

	// ================================================================
	// Security Hardening: OAuth2, ABAC, Audit, mTLS, Gateway, Vault
	// ================================================================

	// Initialize OAuth2/OIDC Manager (multi-provider federation)
	oauth2Mgr := auth.NewOAuth2Manager(auth.OAuth2Config{
		AuthService: authService,
		Logger:      logger,
		SessionTTL:  10 * time.Minute,
	})
	_ = oauth2Mgr // OAuth2 routes added to router below
	logger.Info("OAuth2/OIDC manager initialized")

	// Initialize ABAC Engine (attribute-based access control)
	abacEngine := auth.NewABACEngine(auth.ABACConfig{
		Logger: logger,
	})
	_ = abacEngine // ABAC middleware available for route-level enforcement
	logger.Info("ABAC engine initialized with default policies")

	// Initialize Fine-Grained Permission Manager
	permManager := auth.NewPermissionManager(auth.PermissionManagerConfig{
		Logger: logger,
	})
	_ = permManager // Permission middleware available for route-level enforcement
	logger.Info("Fine-grained permission manager initialized")

	// Initialize Audit Logging
	auditStore := auth.NewAuditStore(10000)
	auditSink := auth.NewChannelAuditSink(4096)
	logger.Info("Audit logging initialized (in-memory store + async channel)")

	// Initialize mTLS Certificate Manager
	mtlsMgr, mtlsErr := security.NewMTLSManager(security.MTLSConfig{
		CAConfig: security.CAConfig{
			TrustDomain: "cloudai-fusion.local",
		},
		CertDuration: 24 * time.Hour,
		Logger:       logger,
	})
	if mtlsErr != nil {
		logger.WithError(mtlsErr).Warn("Failed to init mTLS manager")
	} else {
		go mtlsMgr.StartRotationLoop(ctx, 1*time.Hour)
		logger.Info("mTLS certificate manager initialized (SPIFFE identities)")
	}

	// Initialize API Gateway Security
	apiGateway := security.NewGateway(security.GatewayConfig{
		EnableWAF:     true,
		EnableIPACL:   false, // Enable in production with proper allowlists
		EnableAPIKeys: true,
		Logger:        logger,
	})
	_ = apiGateway // Gateway middleware applied to router below
	logger.Info("API Gateway security initialized (WAF + API keys)")

	// Initialize Vault Client (secret rotation)
	vaultClient := security.NewVaultClient(security.VaultConfig{
		Logger: logger,
	})
	vaultClient.AddRotationPolicy(&security.RotationPolicy{
		Path: "secret/cloudai", Key: "jwt-secret",
		Interval: 30 * 24 * time.Hour, Generator: "random", Length: 64,
	})
	vaultClient.AddRotationPolicy(&security.RotationPolicy{
		Path: "secret/cloudai", Key: "db-password",
		Interval: 7 * 24 * time.Hour, Generator: "random", Length: 32,
	})
	go vaultClient.StartRotationLoop(ctx, 5*time.Minute)
	logger.Info("Vault client initialized (secret rotation active)")

	// Initialize Supply Chain Security (Sigstore/Cosign)
	supplyChain := security.NewSupplyChainManager(security.SupplyChainConfig{
		Logger: logger,
	})
	_ = supplyChain
	logger.Info("Supply chain security initialized (Sigstore policies)")

	// Initialize Network Policy Engine
	netPolicyEngine := security.NewNetworkPolicyEngine(security.NetworkPolicyEngineConfig{
		Logger: logger,
	})
	_ = netPolicyEngine
	logger.Info("Network policy automation engine initialized")

	// Setup Gin router
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create API router (with infrastructure components injected)
	routerCfg := api.RouterConfig{
		AuthService:     authService,
		CloudManager:    cloudManager,
		ClusterManager:  clusterManager,
		SecurityManager: securityManager,
		MonitorService:  monitorService,
		WorkloadManager: workloadManager,
		MeshManager:     meshManager,
		WasmManager:     wasmManager,
		EdgeManager:     edgeManager,
		Logger:          logger,
		Store:           dbStore,
		FeatureFlags:    featureFlags,
		WebSocketHub:    wsHub,
		ControllerMgr:   ctrlManager,
	}
	if tracingProvider != nil {
		routerCfg.Tracer = tracingProvider.Tracer()
	}
	router := api.NewRouter(routerCfg)

	// Apply API Gateway security middleware (WAF, IP ACL, API key validation)
	router.Use(apiGateway.GatewayMiddleware())

	// Apply audit logging middleware
	router.Use(auth.AuditMiddleware(auth.AuditConfig{
		Level:  auth.AuditLevelRequest,
		Sinks:  []auth.AuditSink{auditStore, auditSink, auth.NewLoggerAuditSink(logger)},
		Logger: logger,
		SkipPaths: []string{"/healthz", "/readyz", "/metrics"},
	}))

	// Apply golden signal metrics middleware
	router.Use(metrics.GinMiddleware(metrics.GinMiddlewareConfig{
		ServiceName: "apiserver",
		SLOTracker:  sloTracker,
	}))

	// OAuth2/OIDC routes (unauthenticated)
	oauthGroup := router.Group("/auth/oauth2")
	{
		oauthGroup.GET("/login", oauth2Mgr.LoginHandler())
		oauthGroup.GET("/callback", oauth2Mgr.CallbackHandler())
		oauthGroup.GET("/providers", oauth2Mgr.ProvidersHandler())
	}

	// Audit log query endpoints (admin only)
	auditHandlers := auth.NewAuditHandlers(auditStore)
	router.GET("/admin/audit/recent", auditHandlers.RecentHandler())
	router.GET("/admin/audit/query", auditHandlers.QueryHandler())

	// Expose dynamic log level endpoint
	router.Any("/admin/log-level", gin.WrapH(appLogger.LevelHandler()))

	// Security status endpoint
	router.GET("/admin/security/status", func(c *gin.Context) {
		status := map[string]interface{}{
			"gateway":       apiGateway.Status(),
			"vault":         vaultClient.Status(),
			"supply_chain":  supplyChain.Status(),
			"net_policy":    netPolicyEngine.Status(),
			"frameworks":    security.SupportedFrameworks(),
		}
		if mtlsMgr != nil {
			status["mtls"] = mtlsMgr.Status()
		}
		c.JSON(200, gin.H{"security": status})
	})

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// Start gRPC server in background with tracing interceptors
	grpcServer := rpcserver.New(rpcserver.Config{
		Port: cfg.MetricsPort + 1, // metrics_port+1 for gRPC
	}, logger)
	go func() {
		if err := grpcServer.Start(); err != nil {
			logger.WithError(err).Error("gRPC server failed")
		}
	}()
	logger.WithField("port", cfg.MetricsPort+1).Info("gRPC server started")

	// Start HTTP server in goroutine
	go func() {
		logger.WithField("addr", addr).Info("API Server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Server failed to start")
		}
	}()

	// Graceful shutdown — wait for signal via signal.NotifyContext (Problem #8.2)
	<-ctx.Done()
	stop() // reset signal handler
	logger.Info("Received shutdown signal, starting graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Shutdown HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("HTTP server forced to shutdown")
	}

	// Shutdown gRPC server
	grpcServer.Stop()

	// Shutdown control plane (replaces direct controller manager stop)
	ctrlPlane.Stop()

	// Stop observability components
	sloTracker.Stop()
	resCollector.Stop()

	logger.Info("API Server stopped gracefully")
	return nil
}
