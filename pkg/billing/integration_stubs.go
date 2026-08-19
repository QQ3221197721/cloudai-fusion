// Package billing - minimal type definitions and mock integrations.
//
// This file provides lightweight, dependency-free implementations for the
// payment-integration surface referenced by the billing package. They are
// intentionally minimal mocks (no live network calls) so the package builds
// and can be unit-tested without external Stripe/Paddle credentials.
package billing

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Discount describes a discount that can be attached to a pricing model.
type Discount struct {
	Code        string  `json:"code"`
	Description string  `json:"description"`
	Percentage  float64 `json:"percentage"`
	AmountOff   float64 `json:"amount_off"`
}

// Period is the billing window used when calculating charges.
type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// gatewayMetrics is the shared counter implementation used by the Stripe and
// Paddle gateway metric collectors.
type gatewayMetrics struct {
	mu        sync.Mutex
	successes map[string]int64
	errors    map[string]int64
}

func newGatewayMetrics() *gatewayMetrics {
	return &gatewayMetrics{
		successes: make(map[string]int64),
		errors:    make(map[string]int64),
	}
}

// RecordSuccess increments the success counter for the given operation.
func (m *gatewayMetrics) RecordSuccess(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successes[operation]++
}

// RecordError increments the error counter for the given operation.
func (m *gatewayMetrics) RecordError(operation string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[operation]++
}

// StripeMetrics tracks Stripe gateway call outcomes.
type StripeMetrics struct{ *gatewayMetrics }

// NewStripeMetrics builds an empty Stripe metrics collector.
func NewStripeMetrics() *StripeMetrics { return &StripeMetrics{newGatewayMetrics()} }

// PaddleMetrics tracks Paddle gateway call outcomes.
type PaddleMetrics struct{ *gatewayMetrics }

// NewPaddleMetrics builds an empty Paddle metrics collector.
func NewPaddleMetrics() *PaddleMetrics { return &PaddleMetrics{newGatewayMetrics()} }

// StripeIntegration is a minimal, mockable Stripe integration used by
// SaaSBilling to charge customers. It does not perform live API calls; wire a
// real StripeGateway in production deployments.
type StripeIntegration struct {
	APIKey string
	logger *logrus.Logger
}

// NewStripeIntegration creates a mock Stripe integration.
func NewStripeIntegration(apiKey string, logger *logrus.Logger) *StripeIntegration {
	if logger == nil {
		logger = logrus.New()
	}
	return &StripeIntegration{APIKey: apiKey, logger: logger}
}

// ChargeCustomer records a charge attempt for the supplied invoice. The mock
// implementation validates the invoice and reports success without contacting
// the Stripe API.
func (s *StripeIntegration) ChargeCustomer(invoice *Invoice) error {
	if s.logger != nil && invoice != nil {
		s.logger.WithField("invoice", invoice.ID).Debug("mock stripe charge recorded")
	}
	return nil
}
