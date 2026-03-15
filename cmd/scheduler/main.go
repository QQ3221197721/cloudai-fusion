// Package main - CloudAI Fusion Resource Scheduler
// Intelligent AI workload scheduler with GPU topology awareness,
// reinforcement learning-based optimization, and heterogeneous
// resource management capabilities.
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

	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "cloudai-scheduler",
		Short: "CloudAI Fusion Resource Scheduler",
		Long: `CloudAI Fusion Resource Scheduler - Intelligent AI workload
scheduler with GPU topology awareness, RL-based optimization,
and heterogeneous resource management.`,
		RunE: runScheduler,
	}

	rootCmd.Flags().String("config", "", "config file path")
	rootCmd.Flags().String("host", "0.0.0.0", "scheduler listen host")
	rootCmd.Flags().Int("port", 8081, "scheduler listen port")
	rootCmd.Flags().String("log-level", "info", "log level")
	rootCmd.Flags().String("ai-engine", "localhost:8090", "AI engine service address")
	rootCmd.Flags().Int("metrics-port", 9101, "prometheus metrics port")
	rootCmd.Flags().Int("scheduling-interval", 10, "scheduling loop interval in seconds")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("CloudAI Fusion Scheduler\n")
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

func runScheduler(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cmd)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := initLogger(cfg.LogLevel)
	logger.WithFields(logrus.Fields{
		"version": Version,
	}).Info("Starting CloudAI Fusion Scheduler")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize scheduler engine
	engine, err := scheduler.NewEngine(scheduler.EngineConfig{
		DatabaseURL:        cfg.DatabaseURL(),
		RedisAddr:          cfg.RedisAddr,
		KafkaBrokers:       cfg.KafkaBrokers,
		AIEngineAddr:       cfg.AIEngineAddr,
		SchedulingInterval: time.Duration(cfg.SchedulingInterval) * time.Second,
		Logger:             logger,
	})
	if err != nil {
		return fmt.Errorf("failed to init scheduler engine: %w", err)
	}

	// Start scheduling loop
	go engine.Run(ctx)
	logger.Info("Scheduler engine started")

	// Setup API for scheduler control
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	// Health & readiness
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy", "component": "scheduler"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if engine.IsReady() {
			c.JSON(http.StatusOK, gin.H{"status": "ready"})
		} else {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready"})
		}
	})

	// Scheduler APIs
	schedulerAPI := router.Group("/api/v1/scheduler")
	{
		schedulerAPI.GET("/status", engine.HandleStatus)
		schedulerAPI.GET("/queue", engine.HandleQueue)
		schedulerAPI.POST("/schedule", engine.HandleScheduleRequest)
		schedulerAPI.POST("/preempt", engine.HandlePreemptRequest)
		schedulerAPI.GET("/topology", engine.HandleGPUTopology)
		schedulerAPI.GET("/utilization", engine.HandleUtilization)
		schedulerAPI.PUT("/policy", engine.HandleUpdatePolicy)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.SchedulerPort)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	go func() {
		logger.WithField("addr", addr).Info("Scheduler API listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Fatal("Scheduler API failed")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down scheduler...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	engine.Stop()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("scheduler shutdown error: %w", err)
	}

	cancel()
	logger.Info("Scheduler stopped gracefully")
	return nil
}

func initLogger(level string) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	lvl, err := logrus.ParseLevel(level)
	if err != nil {
		lvl = logrus.InfoLevel
	}
	logger.SetLevel(lvl)
	return logger
}
