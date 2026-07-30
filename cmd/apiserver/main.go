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
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/controlplane"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/election"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/finops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/hunt"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/logging"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/mesh"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/messaging"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/metrics"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/migrate"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/monitor"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/resilience"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/rpcserver"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tracing"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/websocket"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wellreadiness"
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

	// Establish the run-mode policy that governs simulated-backend enforcement.
	// In production, subsystems that can only offer a simulated/in-memory backend
	// cause the process to refuse to boot (see capability.Enforce below).
	capability.SetPolicy(cfg.EffectiveRunMode())
	logger.WithField("run_mode", cfg.EffectiveRunMode().String()).Info("Run mode established")

	// Graceful shutdown context using signal.NotifyContext (Problem #8.2)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize OpenTelemetry tracing (enhanced: adaptive sampling + profiling)
	tracingProvider, err := tracing.Init(ctx, tracing.Config{
		ServiceName:       "cloudai-apiserver",
		ServiceVersion:    Version,
		Endpoint:          cfg.JaegerEndpoint,
		SampleRate:        0.1,
		Enabled:           cfg.JaegerEndpoint != "",
		AdaptiveSampling:  true,
		MinSampleRate:     0.01,
		MaxSampleRate:     1.0,
		TargetSpansPerSec: 200,
		Environment:       cfg.Env,
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
		defer func() { _ = dbStore.Close() }()
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
		Mode:            mesh.MeshModeAmbient,
		EnableMTLS:      true,
		EnableTracing:   true,
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
		CloudEndpoint:      fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port),
		MaxEdgePowerWatts:  200,
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
	defer func() { _ = bus.Close() }()
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
	defer func() { _ = msgProducer.Close() }()
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
		EnableIPACL:   cfg.GatewayEnableIPACL, // off by default; operators enable explicitly
		EnableAPIKeys: true,
		Logger:        logger,
	})
	_ = apiGateway // Gateway middleware applied to router below
	logger.WithField("ip_acl", cfg.GatewayEnableIPACL).
		Info("API Gateway security initialized (WAF + API keys)")

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

	// ================================================================
	// Verifiable Control Plane: signed, hash-chained evidence ledger
	// ================================================================
	// This is the platform's "prove it" spine: consequential actions emit a
	// signed receipt that states which real-vs-simulated backend executed them.
	// Build() reports its signer/ledger/anchor backends to pkg/capability, so a
	// production boot with an ephemeral key or in-memory ledger fails Enforce().
	var evidenceKeyPEM []byte
	if cfg.EvidenceKeyPath != "" {
		evidenceKeyPEM, err = os.ReadFile(cfg.EvidenceKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read evidence signing key %q: %w", cfg.EvidenceKeyPath, err)
		}
	}
	evidenceBuild := evidence.BuildConfig{
		SigningKeyPEM: evidenceKeyPEM,
		RekorURL:      cfg.RekorURL,
		Logger:        logger,
	}
	if dbStore != nil {
		evidenceBuild.DB = dbStore.DB()
	}
	evidenceLedger, err := evidence.Build(evidenceBuild)
	if err != nil {
		return fmt.Errorf("failed to init evidence ledger: %w", err)
	}
	logger.WithField("key_id", evidenceLedger.Signer().KeyID()).Info("Evidence ledger (Verifiable Control Plane) initialized")

	// Provable FinOps: measured GPU reclamation with signed savings receipts. The
	// reclaim action defaults to an honest simulation until a real K8s/cloud
	// backend is wired; each receipt records whether the reclaim actually ran.
	finopsReclaimer := finops.NewReclaimEngine(finops.ReclaimEngineConfig{
		Recorder: evidenceLedger,
		Logger:   logger,
	})
	logger.Info("FinOps reclaim engine initialized (measured savings receipts)")

	// Verifiable AI Red Team: authorized, evidence-grade security validation.
	// Engagements and scope-gated actions record signed receipts into the SAME
	// evidence ledger, so red-team reports are offline-verifiable.
	redteamManager := redteam.NewManager(evidenceLedger, logger)
	logger.Info("Verifiable AI Red Team subsystem initialized (evidence-backed)")

	// AISecOps L1 Threat Intelligence store. Prefer the real ClickHouse TSDB when
	// an endpoint is configured and reachable; otherwise fall back to the honest
	// in-memory (simulated) store. The store's real-vs-simulated nature is
	// reported to pkg/capability so a production boot requires a real backend.
	var intelStore intel.Store = intel.NewMemoryStore()
	if cfg.ClickHouseEndpoint != "" {
		chStore, chErr := intel.NewClickHouseStore(intel.ClickHouseConfig{
			Endpoint: cfg.ClickHouseEndpoint,
			Database: cfg.ClickHouseDB,
			User:     cfg.ClickHouseUser,
			Password: cfg.ClickHousePassword,
		})
		if chErr != nil {
			logger.WithError(chErr).Warn("ClickHouse unavailable - L1 intel falling back to in-memory (simulated)")
		} else {
			intelStore = chStore
			logger.Info("L1 threat-intelligence store: ClickHouse (real)")
		}
	}
	if err := capability.MustReal("intel.store", intelStore.Driver(), intelStore.IsReal(),
		"L1 threat-intelligence store"); err != nil {
		return fmt.Errorf("intel store capability: %w", err)
	}

	// AISecOps Operations layer (L3-L8): endpoint/network/workload/identity/image
	// detection + SOAR response. Analyses and responses are signed into the SAME
	// evidence ledger, so findings and response decisions are offline-verifiable.
	socEngine := soc.NewEngine(intelStore, logger)
	socEngine.SetEvidenceRecorder(evidenceLedger)
	// Wire the REAL L8 actuator backed by existing security subsystems (gateway IP
	// ACL + network-policy engine) instead of the placeholder recorder, so
	// automated responses actually enforce/create control-plane objects.
	socActuator := newNetworkPolicyActuator(apiGateway, netPolicyEngine)
	// Opt-in data-plane closure: when enabled and a cluster is reachable, L8
	// isolate/harden responses are applied as real networkingv1.NetworkPolicy
	// objects (CNI-enforced), completing the detect→decide→enforce loop.
	if cfg.SOARClusterApply {
		if applier := buildSOARClusterApplier(logger); applier != nil {
			socActuator.SetClusterApplier(applier)
		}
	}
	socEngine.SetActuator(socActuator)
	logger.WithField("actuator_real", socActuator.IsReal()).
		Info("AISecOps SOC subsystem initialized (L3-L8, evidence-backed)")

	// L3 EDR collector: the real /proc collector (Linux) gathers running-process
	// executable hashes for endpoint detection; otherwise a simulated collector.
	var edrCollector soc.EDRCollector
	if cfg.EDRRealCollector {
		edrCollector = soc.NewProcEDRCollector("")
		logger.WithField("real", edrCollector.IsReal()).Info("L3 EDR collector: proc-edr")
	} else {
		edrCollector = soc.NewStaticEDRCollector(soc.EndpointTelemetry{})
		logger.Info("L3 EDR collector: static (simulated)")
	}

	// ================================================================
	// AISecOps deep-well fabric (EventBus v2) + well-readiness honesty
	// ================================================================
	wellreadiness.SetPolicy(cfg.EffectiveRunMode())

	// Instantiate the well router so an event raised by one well really
	// propagates to the wells that must react (L1→L2/L3/L4/L14; L3-L7→L8; ...),
	// bounded by a hop cap. This is what makes "16 wells connected" a fact.
	wellRouter := eventbus.NewWellRouter(bus, 4, logger)
	if rErr := wellRouter.Connect(ctx); rErr != nil {
		logger.WithError(rErr).Warn("well router connect failed")
	}

	// L1 Threat-Intelligence Hub over the shared store: offline sync + fabric emit.
	intelHub := intel.NewHub(nil, intelStore, logger)
	intelHub.SetEvidenceRecorder(evidenceLedger)
	intelHub.SetWellPublisher(func(c context.Context, kind string, detail map[string]any) {
		_ = eventbus.PublishWellEvent(c, bus, eventbus.WellIntel, kind, detail)
	})

	// L2 Threat-Hunting engine over the shared store: correlation + fabric emit.
	huntEngine := hunt.NewEngine(intelStore, nil, logger)
	huntEngine.SetEvidenceRecorder(evidenceLedger)
	huntEngine.SetWellPublisher(func(c context.Context, kind string, detail map[string]any) {
		_ = eventbus.PublishWellEvent(c, bus, eventbus.WellHunt, kind, detail)
	})

	// L3-L8 SOC engine: findings escalate onto the fabric toward L8.
	socEngine.SetWellPublisher(func(c context.Context, well int, kind string, detail map[string]any) {
		_ = eventbus.PublishWellEvent(c, bus, eventbus.DeepWell(well), kind, detail)
	})

	// L8 auto-consumer: subscribe once to the fabric and, for events routed to
	// WellResponse (L8), run the SOAR response for the escalated findings. This
	// closes the detection→response loop: an L3-L7 detection now automatically
	// drives an evidence-signed L8 response, no manual API call required.
	if _, subErr := bus.Subscribe(eventbus.TopicWellEvent, func(c context.Context, ev *eventbus.Event) error {
		w, ok := eventbus.WellOf(ev)
		if !ok || w != eventbus.WellResponse {
			return nil
		}
		var we eventbus.WellEvent
		if err := ev.UnmarshalData(&we); err != nil {
			return nil
		}
		ids, _ := we.Detail["finding_ids"].(string)
		if ids == "" {
			return nil
		}
		socEngine.OnEscalation(c, strings.Split(ids, ","))
		return nil
	}); subErr != nil {
		logger.WithError(subErr).Warn("L8 auto-consumer subscribe failed")
	}

	// Report each wired well's HONEST readiness. The maturity claim is derived
	// from facts (real backend? fabric-connected?), never hand-written; an
	// overclaim would fail wellreadiness.Enforce() below in production.
	reportWell := func(well int, name string, realBackend, fabric, evidenceBacked bool) {
		mode := wellreadiness.BackendSimulated
		claim := wellreadiness.M1Wired
		if realBackend {
			mode = wellreadiness.BackendReal
			claim = wellreadiness.M2RealBackend
			if fabric {
				claim = wellreadiness.M3FabricConnected
			}
		}
		_ = wellreadiness.Report(wellreadiness.Status{
			Well: well, Name: name, Claimed: claim, Wired: true,
			BackendMode: mode, FabricConnected: fabric, EvidenceBacked: evidenceBacked,
		})
	}
	reportWell(1, "L1-intel", intelStore.IsReal(), true, true)
	reportWell(2, "L2-hunt", false, true, true) // heuristic reasoner (simulated backend)
	reportWell(3, "L3-endpoint", edrCollector.IsReal(), true, true)
	reportWell(4, "L4-network", false, true, true)
	reportWell(5, "L5-workload", false, true, true)
	reportWell(6, "L6-identity", false, true, true)
	reportWell(7, "L7-image", false, true, true)
	// L8 now genuinely CONSUMES fabric escalations and EXECUTES responses via the
	// real network-policy actuator (gateway IP-ACL + policy engine). Its backend
	// mode reflects whether a real data-plane enforcement path is active.
	l8Backend := wellreadiness.BackendSimulated
	l8Claim := wellreadiness.M1Wired
	if socActuator.IsReal() {
		l8Backend = wellreadiness.BackendReal
		l8Claim = wellreadiness.M2RealBackend
	}
	_ = wellreadiness.Report(wellreadiness.Status{
		Well: 8, Name: "L8-response", Claimed: l8Claim, Wired: true,
		BackendMode: l8Backend, FabricConnected: true, EvidenceBacked: true,
		Detail: "auto-consumes L3-L7 escalations; executes via network-policy actuator (gateway IP-ACL + policy engine)",
	})
	// L13 evidence: always-real crypto, fabric-reachable (L8→L13 edge), and its
	// third-party OFFLINE verifiability is CI-verified by `cafctl moat-demo`
	// (cmd/cafctl TestMoatDemo_*). It therefore honestly reaches M4-ci-verified.
	_ = wellreadiness.Report(wellreadiness.Status{
		Well: 13, Name: "L13-evidence", Claimed: wellreadiness.M4CIVerified,
		Wired: true, BackendMode: wellreadiness.BackendReal, FabricConnected: true, EvidenceBacked: true,
		Detail: "Ed25519+Merkle; offline third-party verification CI-verified (cafctl moat-demo)",
	})
	reportWell(14, "L14-redteam", false, false, true)
	// L9 Data Storage: the shared GORM store (also backs the evidence ledger, WAL
	// and mesh manager). Real SQL backend whenever the DB initialized above.
	reportWell(9, "L9-data", dbStore != nil, false, true)
	// L15 FinOps: the reclaim engine is wired; its default cost model is the
	// static table (simulated) until a live cloud-pricing source is configured.
	reportWell(15, "L15-finops", false, false, true)
	// L16 Network Policy: mesh manager + network-policy actuator are wired; the
	// backend is real when the actuator has a live enforcement path (the same
	// fact L8 uses, so the two stay consistent).
	reportWell(16, "L16-netpolicy", socActuator.IsReal(), false, true)
	// L10-L12 (Compute/RL, Model Registry, Inference) are delivered by the Python
	// ai/ sidecar (ai/scheduler, ai/agents), NOT wired into this Go control plane.
	// Report them honestly as scaffold from the API server's perspective rather
	// than overclaiming a maturity this process cannot attest — the wellreadiness
	// contract forbids claiming a level we cannot structurally prove.
	for _, sc := range []struct {
		well int
		name string
	}{{10, "L10-compute"}, {11, "L11-model"}, {12, "L12-inference"}} {
		_ = wellreadiness.Report(wellreadiness.Status{
			Well: sc.well, Name: sc.name, Claimed: wellreadiness.M0Scaffold,
			Wired: false, BackendMode: wellreadiness.BackendNone, FabricConnected: false,
			Detail: "delivered by the Python ai/ sidecar (ai/scheduler, ai/agents); not wired into the Go control plane",
		})
	}
	logger.WithField("wells", len(wellreadiness.Snapshot())).Info("AISecOps well-readiness reported")

	// Fail fast: in production, refuse to boot if any initialized subsystem is
	// backed by a simulation instead of a real dependency. This is the systemic
	// cure for the previous "boots green on fakes" behavior.
	if err := capability.Enforce(); err != nil {
		return fmt.Errorf("startup blocked by run_mode policy: %w", err)
	}
	logger.WithField("run_mode", capability.Policy().String()).Info("Run-mode capability check passed")

	// Well-layer honesty backstop: in production, refuse to boot if any deep well
	// overclaims its maturity (e.g. claims fabric-connected while unwired).
	if err := wellreadiness.Enforce(); err != nil {
		return fmt.Errorf("startup blocked by well-readiness policy: %w", err)
	}

	// Setup Gin router
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Contrib plugin runtime (render-farm / disaster-recovery / customer-service).
	// Inert unless configured; when active, plugins run under full lifecycle
	// management (Init→Start→health→Stop) and surface via /api/v1/plugins.
	pluginManager, err := setupContribPlugins(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("contrib plugins: %w", err)
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
		EvidenceLedger:  evidenceLedger,
		FinOpsReclaimer: finopsReclaimer,
		RedTeamManager:  redteamManager,
		SOCEngine:       socEngine,
		SOCEDRCollector: edrCollector,
		HuntEngine:      huntEngine,
		IntelHub:        intelHub,
		PluginManager:   pluginManager,
	}
	if tracingProvider != nil {
		routerCfg.Tracer = tracingProvider.Tracer()
	}
	router := api.NewRouter(routerCfg)

	// Apply API Gateway security middleware (WAF, IP ACL, API key validation)
	router.Use(apiGateway.GatewayMiddleware())

	// Apply audit logging middleware
	router.Use(auth.AuditMiddleware(auth.AuditConfig{
		Level:     auth.AuditLevelRequest,
		Sinks:     []auth.AuditSink{auditStore, auditSink, auth.NewLoggerAuditSink(logger)},
		Logger:    logger,
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
			"gateway":      apiGateway.Status(),
			"vault":        vaultClient.Status(),
			"supply_chain": supplyChain.Status(),
			"net_policy":   netPolicyEngine.Status(),
			"frameworks":   security.SupportedFrameworks(),
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

	// Stop contrib plugins (reverse dependency order)
	if pluginManager != nil {
		if err := pluginManager.StopAll(shutdownCtx); err != nil {
			logger.WithError(err).Error("contrib plugin shutdown reported errors")
		}
	}

	// Stop observability components
	sloTracker.Stop()
	resCollector.Stop()

	logger.Info("API Server stopped gracefully")
	return nil
}
