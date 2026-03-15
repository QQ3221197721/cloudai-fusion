// Package rpcserver provides a gRPC server for internal service-to-service
// communication within the CloudAI Fusion platform. Used for scheduler ↔ agent
// and apiserver ↔ scheduler communication with protobuf-like efficiency.
//
// Note: This implementation uses manual gRPC registration without .proto files
// for simplicity. In production, use protoc-generated stubs.
package rpcserver

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// Config configures the gRPC server.
type Config struct {
	Port                int
	MaxRecvMsgSize      int           // bytes, default 16MB
	MaxSendMsgSize      int           // bytes, default 16MB
	KeepaliveInterval   time.Duration // default 30s
	KeepaliveTimeout    time.Duration // default 10s
	MaxConcurrentStreams uint32        // default 1000
}

// DefaultConfig returns sensible defaults for the gRPC server.
func DefaultConfig() Config {
	return Config{
		Port:                9090,
		MaxRecvMsgSize:      16 * 1024 * 1024,
		MaxSendMsgSize:      16 * 1024 * 1024,
		KeepaliveInterval:   30 * time.Second,
		KeepaliveTimeout:    10 * time.Second,
		MaxConcurrentStreams: 1000,
	}
}

// Server wraps a gRPC server with lifecycle management.
type Server struct {
	grpcServer   *grpc.Server
	healthServer *health.Server
	config       Config
	logger       *logrus.Logger
	mu           sync.Mutex
	running      bool
}

// New creates a new gRPC server with the given configuration.
func New(cfg Config, logger *logrus.Logger) *Server {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	if cfg.MaxRecvMsgSize == 0 {
		cfg.MaxRecvMsgSize = 16 * 1024 * 1024
	}
	if cfg.MaxSendMsgSize == 0 {
		cfg.MaxSendMsgSize = 16 * 1024 * 1024
	}
	if cfg.KeepaliveInterval == 0 {
		cfg.KeepaliveInterval = 30 * time.Second
	}
	if cfg.KeepaliveTimeout == 0 {
		cfg.KeepaliveTimeout = 10 * time.Second
	}
	if cfg.MaxConcurrentStreams == 0 {
		cfg.MaxConcurrentStreams = 1000
	}

	opts := []grpc.ServerOption{
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(cfg.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(cfg.MaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.KeepaliveInterval,
			Timeout: cfg.KeepaliveTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		// Interceptors for logging and metrics
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor(logger),
			recoveryUnaryInterceptor(logger),
		),
		grpc.ChainStreamInterceptor(
			loggingStreamInterceptor(logger),
		),
	}

	gs := grpc.NewServer(opts...)

	// Register health check service
	hs := health.NewServer()
	healthpb.RegisterHealthServer(gs, hs)
	hs.SetServingStatus("cloudai-fusion", healthpb.HealthCheckResponse_SERVING)

	// Enable server reflection for grpcurl / debugging
	reflection.Register(gs)

	return &Server{
		grpcServer:   gs,
		healthServer: hs,
		config:       cfg,
		logger:       logger,
	}
}

// GRPCServer returns the underlying *grpc.Server for service registration.
func (s *Server) GRPCServer() *grpc.Server {
	return s.grpcServer
}

// Start begins listening on the configured port. Blocks until Stop() is called.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("gRPC server already running")
	}
	s.running = true
	s.mu.Unlock()

	addr := fmt.Sprintf(":%d", s.config.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.logger.WithField("addr", addr).Info("gRPC server listening")

	if err := s.grpcServer.Serve(lis); err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return fmt.Errorf("gRPC server failed: %w", err)
	}
	return nil
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.healthServer.SetServingStatus("cloudai-fusion", healthpb.HealthCheckResponse_NOT_SERVING)
	s.grpcServer.GracefulStop()
	s.running = false
	s.logger.Info("gRPC server stopped")
}

// ============================================================================
// Interceptors
// ============================================================================

func loggingUnaryInterceptor(logger *logrus.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		fields := logrus.Fields{
			"method":   info.FullMethod,
			"duration": duration.String(),
		}
		if err != nil {
			fields["error"] = err.Error()
			logger.WithFields(fields).Warn("gRPC request failed")
		} else {
			logger.WithFields(fields).Debug("gRPC request")
		}
		return resp, err
	}
}

func recoveryUnaryInterceptor(logger *logrus.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.WithFields(logrus.Fields{
					"method": info.FullMethod,
					"panic":  fmt.Sprintf("%v", r),
				}).Error("gRPC handler panicked")
				err = fmt.Errorf("internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

func loggingStreamInterceptor(logger *logrus.Logger) grpc.StreamServerInterceptor {
	return func(
		srv interface{},
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		err := handler(srv, ss)
		duration := time.Since(start)

		fields := logrus.Fields{
			"method":   info.FullMethod,
			"duration": duration.String(),
		}
		if err != nil {
			fields["error"] = err.Error()
			logger.WithFields(fields).Warn("gRPC stream failed")
		} else {
			logger.WithFields(fields).Debug("gRPC stream completed")
		}
		return err
	}
}
