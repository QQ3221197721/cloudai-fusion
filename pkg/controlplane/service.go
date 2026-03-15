// Package controlplane implements the independent control plane service for
// CloudAI Fusion. It decouples the reconciliation loop orchestration from
// the API server, enabling the control plane to run as a standalone process.
//
// The control plane communicates with other services exclusively through:
//   - EventBus: for event-driven notifications (async)
//   - gRPC: for synchronous service-to-service calls
//   - Messaging queue: for durable async command processing
//
// This replaces the tight coupling where apiserver directly imported and
// instantiated controller reconcilers.
package controlplane

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/controller"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/election"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
)

// ============================================================================
// Control Plane Service
// ============================================================================

// Config holds configuration for the control plane service.
type Config struct {
	// Logger for structured logging.
	Logger *logrus.Logger

	// EventBus for publishing/subscribing to domain events.
	EventBus eventbus.EventBus

	// MaxConcurrentReconciles per controller.
	MaxConcurrentReconciles int

	// SyncPeriod for full resync of all controllers.
	SyncPeriod time.Duration

	// LeaderElection enables HA leader election.
	LeaderElection bool

	// LeaderElectionConfig for custom leader election parameters.
	LeaderElectionConfig *election.Config

	// GRPCPort for the control plane gRPC server.
	GRPCPort int

	// HealthPort for health check HTTP endpoint.
	HealthPort int
}

// Service is the independent control plane service.
// It manages controller lifecycle, watches for events from the event bus,
// and publishes reconciliation results back to the bus.
type Service struct {
	ctrlManager *controller.Manager
	eventBus    eventbus.EventBus
	config      Config
	logger      *logrus.Logger

	// Event-driven triggers: maps event topics to controller names
	eventTriggers map[string]string

	// Service state
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.RWMutex
}

// New creates a new control plane service.
func New(cfg Config) *Service {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	if cfg.MaxConcurrentReconciles <= 0 {
		cfg.MaxConcurrentReconciles = 2
	}
	if cfg.SyncPeriod <= 0 {
		cfg.SyncPeriod = 10 * time.Minute
	}

	// Create the underlying controller manager
	ctrlMgr := controller.NewManager(controller.ManagerConfig{
		Logger:                  logger,
		MaxConcurrentReconciles: cfg.MaxConcurrentReconciles,
		SyncPeriod:              cfg.SyncPeriod,
		LeaderElection:          cfg.LeaderElection,
		LeaderElectionConfig:    cfg.LeaderElectionConfig,
	})

	return &Service{
		ctrlManager:   ctrlMgr,
		eventBus:      cfg.EventBus,
		config:        cfg,
		logger:        logger,
		eventTriggers: make(map[string]string),
	}
}

// ControllerManager returns the underlying controller manager for reconciler registration.
func (s *Service) ControllerManager() *controller.Manager {
	return s.ctrlManager
}

// RegisterEventTrigger maps an event bus topic to a controller name.
// When an event matching the topic arrives, the control plane will
// enqueue a reconciliation request for the specified controller.
func (s *Service) RegisterEventTrigger(eventTopic, controllerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventTriggers[eventTopic] = controllerName
}

// Start starts the control plane service:
// 1. Subscribes to event bus topics that trigger reconciliation
// 2. Starts the controller manager
// 3. Publishes reconciliation events back to the bus
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("control plane already started")
	}
	s.started = true
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.logger.Info("Starting control plane service")

	// Subscribe to event bus topics
	if s.eventBus != nil {
		for topic, ctrlName := range s.eventTriggers {
			topic := topic
			ctrlName := ctrlName
			_, err := s.eventBus.Subscribe(topic, func(ctx context.Context, event *eventbus.Event) error {
				return s.handleEvent(ctx, event, ctrlName)
			})
			if err != nil {
				s.logger.WithError(err).WithFields(logrus.Fields{
					"topic":      topic,
					"controller": ctrlName,
				}).Warn("Failed to subscribe to event topic")
			} else {
				s.logger.WithFields(logrus.Fields{
					"topic":      topic,
					"controller": ctrlName,
				}).Info("Control plane subscribed to event topic")
			}
		}

		// Subscribe to wildcard topics for cross-cutting concerns
		_, _ = s.eventBus.Subscribe("system.>", func(ctx context.Context, event *eventbus.Event) error {
			s.logger.WithFields(logrus.Fields{
				"event_id": event.ID,
				"topic":    event.Topic,
				"source":   event.Source,
			}).Debug("Control plane received system event")
			return nil
		})
	}

	// Start the controller manager in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.ctrlManager.Start(ctx); err != nil {
			s.logger.WithError(err).Error("Controller manager failed")
		}
	}()

	s.logger.Info("Control plane service started")
	return nil
}

// Stop gracefully stops the control plane service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	s.logger.Info("Stopping control plane service")

	// Stop controller manager
	s.ctrlManager.Stop()

	// Cancel context
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for all goroutines
	s.wg.Wait()

	s.started = false
	s.logger.Info("Control plane service stopped")
}

// handleEvent processes an event from the event bus and enqueues
// a reconciliation request for the appropriate controller.
func (s *Service) handleEvent(ctx context.Context, event *eventbus.Event, controllerName string) error {
	// Extract resource identifier from event data
	var resourceRef struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	if err := event.UnmarshalData(&resourceRef); err != nil {
		s.logger.WithError(err).WithField("event_id", event.ID).Warn("Failed to unmarshal event data for reconciliation")
		return nil // don't retry malformed events
	}

	req := controller.Request{
		Name:      resourceRef.Name,
		ID:        resourceRef.ID,
		Namespace: resourceRef.Namespace,
	}

	if err := s.ctrlManager.Enqueue(controllerName, req); err != nil {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"controller": controllerName,
			"event_id":   event.ID,
			"resource":   req.String(),
		}).Warn("Failed to enqueue reconciliation from event")
		return err
	}

	s.logger.WithFields(logrus.Fields{
		"controller":  controllerName,
		"event_topic": event.Topic,
		"resource":    req.String(),
	}).Debug("Enqueued reconciliation from event")

	// Publish acknowledgment event
	if s.eventBus != nil {
		ackEvent, _ := eventbus.NewEvent(
			"controlplane.reconcile.enqueued",
			"Enqueued",
			"controlplane",
			map[string]string{
				"controller":     controllerName,
				"resource":       req.String(),
				"source_event":   event.ID,
				"correlation_id": event.CorrelationID,
			},
		)
		if ackEvent != nil {
			_ = s.eventBus.Publish(ctx, ackEvent)
		}
	}

	return nil
}

// Status returns the current status of the control plane.
func (s *Service) Status() ServiceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ctrlStatus := s.ctrlManager.Status()

	var busStats *eventbus.BusStats
	if s.eventBus != nil {
		stats := s.eventBus.Stats()
		busStats = &stats
	}

	return ServiceStatus{
		Started:           s.started,
		ControllerStatus:  ctrlStatus,
		EventBusStats:     busStats,
		EventTriggerCount: len(s.eventTriggers),
	}
}

// ServiceStatus describes the overall status of the control plane.
type ServiceStatus struct {
	Started           bool                    `json:"started"`
	ControllerStatus  controller.ManagerStatus `json:"controller_manager"`
	EventBusStats     *eventbus.BusStats       `json:"event_bus,omitempty"`
	EventTriggerCount int                      `json:"event_trigger_count"`
}
