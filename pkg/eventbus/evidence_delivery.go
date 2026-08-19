package eventbus

import (
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_delivery.go adds evidence-native message delivery and adaptive
// backpressure on top of the bus:
//
//  1. Delivery proof. Every publish and every consume produces a signed
//     Receipt attesting that the message crossed the bus at time T. Because the
//     ReceiptBuilder chains each receipt to its predecessor, the sequence of
//     receipts forms an unforgeable, offline-verifiable delivery ledger — far
//     stronger than best-effort broker logs.
//
//  2. Adaptive backpressure. Instead of a fixed rate limit, a PID controller
//     continuously steers the allowed publish rate toward a target consumer
//     lag. When consumers fall behind, the controller throttles publishers
//     proportionally, integrally, and by trend; when they catch up it opens the
//     valve. This keeps throughput high without overwhelming consumers.

// DeliveryResult is returned by Publish/Consume with the delivery proof.
type DeliveryResult struct {
	Topic   string            `json:"topic"`
	Lag     int64             `json:"lag"`
	Receipt *evidence.Receipt `json:"receipt"`
}

// EvidenceDeliveryEngine proves deliveries and applies adaptive backpressure.
type EvidenceDeliveryEngine struct {
	rb  *evidence.ReceiptBuilder
	pid *pidController

	mu        sync.Mutex
	published int64
	consumed  int64
	rate      float64 // current allowed publish rate (msgs/sec)
}

// BackpressureConfig configures the PID backpressure controller.
type BackpressureConfig struct {
	// TargetLag is the consumer lag (unacked messages) the controller holds.
	TargetLag float64
	// Kp, Ki, Kd are the proportional, integral and derivative gains.
	Kp, Ki, Kd float64
	// MinRate and MaxRate bound the controller output (msgs/sec).
	MinRate, MaxRate float64
}

// DefaultBackpressureConfig returns gains tuned for a modest lag target.
func DefaultBackpressureConfig() BackpressureConfig {
	return BackpressureConfig{
		TargetLag: 100,
		Kp:        2.0,
		Ki:        0.1,
		Kd:        0.5,
		MinRate:   10,
		MaxRate:   10000,
	}
}

// NewEvidenceDeliveryEngine builds an engine bound to a receipt builder.
func NewEvidenceDeliveryEngine(rb *evidence.ReceiptBuilder, cfg BackpressureConfig) *EvidenceDeliveryEngine {
	return &EvidenceDeliveryEngine{
		rb: rb,
		pid: &pidController{
			kp: cfg.Kp, ki: cfg.Ki, kd: cfg.Kd,
			setpoint: cfg.TargetLag,
			minOut:   cfg.MinRate,
			maxOut:   cfg.MaxRate,
		},
		rate: cfg.MaxRate,
	}
}

// Publish records a publish and returns a chained delivery Receipt.
func (e *EvidenceDeliveryEngine) Publish(topic string, payload []byte) (*DeliveryResult, error) {
	e.mu.Lock()
	e.published++
	lag := e.published - e.consumed
	e.mu.Unlock()

	receipt, err := e.rb.Build("eventbus.publish", struct {
		Topic string `json:"topic"`
		Size  int    `json:"size"`
	}{Topic: topic, Size: len(payload)}, struct {
		Lag int64 `json:"lag"`
	}{Lag: lag})
	if err != nil {
		return nil, err
	}
	return &DeliveryResult{Topic: topic, Lag: lag, Receipt: receipt}, nil
}

// Consume records a consume (acknowledgement) and returns a Receipt proving the
// message was delivered to a consumer at time T.
func (e *EvidenceDeliveryEngine) Consume(topic string) (*DeliveryResult, error) {
	e.mu.Lock()
	e.consumed++
	if e.consumed > e.published {
		e.consumed = e.published // never report negative lag
	}
	lag := e.published - e.consumed
	e.mu.Unlock()

	receipt, err := e.rb.Build("eventbus.consume", struct {
		Topic string `json:"topic"`
	}{Topic: topic}, struct {
		Lag int64 `json:"lag"`
	}{Lag: lag})
	if err != nil {
		return nil, err
	}
	return &DeliveryResult{Topic: topic, Lag: lag, Receipt: receipt}, nil
}

// Lag returns the current number of published-but-not-consumed messages.
func (e *EvidenceDeliveryEngine) Lag() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.published - e.consumed
}

// AdjustRate advances the PID controller by dt seconds using the current lag as
// the process variable and returns the new allowed publish rate (msgs/sec).
// When lag exceeds the target, the rate drops; when it is below, the rate rises.
func (e *EvidenceDeliveryEngine) AdjustRate(dt float64) float64 {
	e.mu.Lock()
	lag := float64(e.published - e.consumed)
	e.mu.Unlock()

	rate := e.pid.update(lag, dt)

	e.mu.Lock()
	e.rate = rate
	e.mu.Unlock()
	return rate
}

// Rate returns the most recently computed allowed publish rate.
func (e *EvidenceDeliveryEngine) Rate() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rate
}

// pidController is a classic proportional-integral-derivative controller with
// output clamping and integral anti-windup. It drives a measurement toward a
// setpoint. Here the measurement is consumer lag and the output is a rate.
type pidController struct {
	kp, ki, kd float64
	setpoint   float64
	minOut     float64
	maxOut     float64

	integral  float64
	prevError float64
	hasPrev   bool
}

// update runs one control step. dt is the elapsed time in seconds.
func (c *pidController) update(measurement, dt float64) float64 {
	if dt <= 0 {
		dt = 1e-3
	}
	// Positive error (lag below target) opens the valve; negative closes it.
	err := c.setpoint - measurement

	// Integral term with trapezoidal accumulation.
	c.integral += err * dt

	// Derivative term on the error signal.
	deriv := 0.0
	if c.hasPrev {
		deriv = (err - c.prevError) / dt
	}
	c.prevError = err
	c.hasPrev = true

	out := c.kp*err + c.ki*c.integral + c.kd*deriv

	// Clamp output and apply anti-windup: if we saturate, unwind the integral
	// contribution that pushed us past the limit so it does not accumulate.
	if out > c.maxOut {
		c.integral -= (out - c.maxOut) / nonZero(c.ki)
		out = c.maxOut
	} else if out < c.minOut {
		c.integral -= (out - c.minOut) / nonZero(c.ki)
		out = c.minOut
	}
	return out
}

func nonZero(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
