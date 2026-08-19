package eventbus

import (
	"crypto/ed25519"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func newTestDeliveryEngine(t *testing.T) *EvidenceDeliveryEngine {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 3)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("eventbus", key)
	return NewEvidenceDeliveryEngine(rb, DefaultBackpressureConfig())
}

func TestPublish_Consume_ProducesChain(t *testing.T) {
	e := newTestDeliveryEngine(t)

	var receipts []*evidence.Receipt
	for i := 0; i < 5; i++ {
		publish, err := e.Publish("t", []byte{byte(i)})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		receipts = append(receipts, publish.Receipt)
		consume, err := e.Consume("t")
		if err != nil {
			t.Fatalf("consume: %v", err)
		}
		receipts = append(receipts, consume.Receipt)
	}
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		t.Fatalf("chain verify failed: %v", err)
	}
}

func TestAdaptiveBackPressure_PIDMainsTargetLag(t *testing.T) {
	cfg := BackpressureConfig{
		TargetLag: 100,
		Kp:        5.0, Ki: 0.5, Kd: 1.0,
		MinRate:   10, MaxRate: 5000,
	}
	e := newTestDeliveryEngine(t)
	// Reconfigure to use our tuned gains.
	e.pid = &pidController{kp: cfg.Kp, ki: cfg.Ki, kd: cfg.Kd, setpoint: cfg.TargetLag, minOut: cfg.MinRate, maxOut: cfg.MaxRate}

	// Simulate a sudden burst: many publishes without consumes -> high lag.
	for i := 0; i < 1000; i++ {
		_, _ = e.Publish("burst", make([]byte, 8))
	}
	// Run the controller over small timesteps until lag stabilizes near target.
	const steps = 400
	var lag float64
	for s := 0; s < steps; s++ {
		lag = float64(e.Lag())
		rate := e.AdjustRate(1e-1)
		// Consume at rate ≈ clamped output to simulate consumers catching up.
		toConsume := int(rate * 1e-1)
		if int64(toConsume) > e.Lag() {
			toConsume = int(e.Lag())
		}
		for c := 0; c < toConsume; c++ {
			_, _ = e.Consume("burst")
		}
	}
	t.Logf("final lag=%.0f after %d steps, rate=%.0f msg/s", lag, steps, e.Rate())
	// With aggressive PID gains we should keep lag within an order of magnitude of target.
	if lag > cfg.TargetLag*10 || lag < cfg.TargetLag*0.5 {
		t.Errorf("lag drifted too far from target: %.0f vs target=%.0f", lag, cfg.TargetLag)
	}
}
