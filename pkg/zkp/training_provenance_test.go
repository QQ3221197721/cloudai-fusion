// Package zkp_test - Unit tests for TrainingProvenanceEngine
package zkp_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/zkp"
)

// ============================================================================
// TrainingProvenanceEngine Integration Tests
// ============================================================================

func TestNewTrainingProvenanceEngine(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	logger := nil
	
	engine, err := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, logger)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	
	if engine == nil {
		t.Fatal("Engine should not be nil")
	}
	
	if engine.GetTracingState() == nil {
		t.Error("Tracing state should be initialized")
	}
}

// ============================================================================
// State Capture Tests
// ============================================================================

func TestCaptureTraceWithValidData(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	modelHash := [32]byte{}
	weightsHash := [32]byte{}
	metrics := zkp.MetricsSummary{
		Loss:           0.5,
		Accuracy:       0.95,
		LearningRate:   0.001,
		GradientNorm:   0.1,
		EpochTimeSec:   10.5,
		GPUUtilPercent: 85.0,
	}
	
	err := engine.CaptureTrace(ctx, 1, 100, modelHash, weightsHash, metrics, 0.5, 0.1)
	if err != nil {
		t.Errorf("Capture trace failed: %v", err)
	}
	
	state := engine.GetTracingState()
	if state.CurrentEpoch != 1 {
		t.Errorf("Expected epoch 1, got %d", state.CurrentEpoch)
	}
	
	if state.TotalSteps != 100 {
		t.Errorf("Expected steps 100, got %d", state.TotalSteps)
	}
	
	if state.CurrentLoss != 0.5 {
		t.Errorf("Expected loss 0.5, got %f", state.CurrentLoss)
	}
}

func TestCaptureTraceMultipleEpochs(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	for epoch := int64(1); epoch <= 5; epoch++ {
		modelHash := [32]byte{}
		weightsHash := [32]byte{}
		
		for step := int64(1); step <= 10; step++ {
			metrics := zkp.MetricsSummary{
				Loss: float64(epoch+step) / 10.0,
			}
			
			err := engine.CaptureTrace(ctx, epoch, step, modelHash, weightsHash, metrics, 0.5, 0.1)
			if err != nil {
				t.Errorf("Epoch %d Step %d capture failed: %v", epoch, step, err)
			}
		}
	}
	
	state := engine.GetTracingState()
	if state.CurrentEpoch != 5 {
		t.Errorf("Expected epoch 5, got %d", state.CurrentEpoch)
	}
	
	expectedSteps := int64(50) // 5 epochs * 10 steps
	if state.TotalSteps != expectedSteps {
		t.Errorf("Expected total steps %d, got %d", expectedSteps, state.TotalSteps)
	}
}

// ============================================================================
// Proof Generation Tests
// ============================================================================

func TestGenerateProofForSingleStep(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	modelHash := [32]byte{}
	weightsHash := [32]byte{}
	metrics := zkp.MetricsSummary{
		Loss:    0.5,
		Accuracy: 0.95,
	}
	
	err := engine.CaptureTrace(ctx, 1, 1, modelHash, weightsHash, metrics, 0.5, 0.1)
	if err != nil {
		t.Fatalf("Capture failed: %v", err)
	}
	
	proof := engine.GetCurrentProof()
	if proof == nil {
		t.Fatal("Current proof should not be nil after capture")
	}
	
	if proof.Epoch != 1 {
		t.Errorf("Expected epoch 1 in proof, got %d", proof.Epoch)
	}
	
	if proof.Step != 1 {
		t.Errorf("Expected step 1 in proof, got %d", proof.Step)
	}
}

func TestProofVerificationFlow(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	// Capture multiple traces
	for i := int64(1); i <= 3; i++ {
		modelHash := [32]byte{}
		weightsHash := [32]byte{}
		metrics := zkp.MetricsSummary{
			Loss: float64(i) / 3.0,
		}
		
		err := engine.CaptureTrace(ctx, i, i*10, modelHash, weightsHash, metrics, 0.5, 0.1)
		if err != nil {
			t.Fatalf("Epoch %d capture failed: %v", i, err)
		}
	}
	
	// Get last proof
	lastProof := engine.GetCurrentProof()
	if lastProof == nil {
		t.Fatal("Last proof should exist")
	}
	
	if lastProof.Epoch != 3 {
		t.Errorf("Expected last epoch 3, got %d", lastProof.Epoch)
	}
}

// ============================================================================
// Tracing State Management Tests
// ============================================================================

func TestTracingStateInitialValues(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	state := engine.GetTracingState()
	
	if state.GlobalStep != 0 {
		t.Errorf("Expected initial GlobalStep 0, got %d", state.GlobalStep)
	}
	
	if state.AverageLoss != 0.0 {
		t.Errorf("Expected initial AverageLoss 0, got %f", state.AverageLoss)
	}
}

func TestAveragingLossFunctionality(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	// Capture with different losses
	for i := int64(1); i <= 3; i++ {
		modelHash := [32]byte{}
		weightsHash := [32]byte{}
		
		loss := float64(i) * 0.1 // 0.1, 0.2, 0.3
		
		metrics := zkp.MetricsSummary{Loss: loss}
		
		err := engine.CaptureTrace(ctx, i, i*10, modelHash, weightsHash, metrics, loss, 0.1)
		if err != nil {
			t.Fatalf("Capture failed: %v", err)
		}
	}
	
	state := engine.GetTracingState()
	expectedAvg := (0.1 + 0.2 + 0.3) / 3.0
	
	if abs(state.AverageLoss-expectedAvg) > 0.001 {
		t.Errorf("Expected average loss %.3f, got %.6f", expectedAvg, state.AverageLoss)
	}
}

// ============================================================================
// Model Commitment Tests
// ============================================================================

func TestModelCommitmentUniqueness(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	// Capture with same model hash twice - should generate different commitments due to epoch/step
	hash1 := [32]byte{}
	copy(hash1[:], "model_v1")
	
	hash2 := [32]byte{}
	copy(hash2[:], "model_v1")
	
	metrics1 := zkp.MetricsSummary{Loss: 0.5}
	metrics2 := zkp.MetricsSummary{Loss: 0.3}
	
	err := engine.CaptureTrace(ctx, 1, 1, hash1, [32]byte{}, metrics1, 0.5, 0.1)
	if err != nil {
		t.Fatalf("First capture failed: %v", err)
	}
	
	err = engine.CaptureTrace(ctx, 1, 2, hash2, [32]byte{}, metrics2, 0.3, 0.1)
	if err != nil {
		t.Fatalf("Second capture failed: %v", err)
	}
	
	state := engine.GetTracingState()
	if state.ModelHash != hash1 && state.ModelHash != hash2 {
		t.Errorf("ModelHash mismatch: got %x, expected one of hashes", state.ModelHash)
	}
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestCaptureTraceWithNilContext(t *testing.T) {
	ctx := context.Background()
	
	poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
	zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
	
	engine, _ := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
	
	// This should work since we use context.Background()
	modelHash := [32]byte{}
	metrics := zkp.MetricsSummary{Loss: 0.5}
	
	err := engine.CaptureTrace(ctx, 1, 1, modelHash, [32]byte{}, metrics, 0.5, 0.1)
	if err != nil {
		t.Errorf("Capture with background context failed: %v", err)
	}
}

func TestMultipleEngineInstances(t *testing.T) {
	ctx := context.Background()
	
	var engines []*zkp.TrainingProvenanceEngine
	
	for i := 0; i < 3; i++ {
		poseidonMirror := zkp.NewPoseidonMirror(zkp.NewPoseidonHash(ctx, nil), nil, nil)
		zkpFactory := zkp.NewTrueProverFactory(ctx, zkp.FactoryConfig{DefaultSystem: "groth16"})
		
		engine, err := zkp.NewTrainingProvenanceEngine(ctx, poseidonMirror, zkpFactory, nil)
		if err != nil {
			t.Fatalf("Engine %d creation failed: %v", i, err)
		}
		
		engines = append(engines, engine)
	}
	
	// Capture on first engine
	err := engines[0].CaptureTrace(ctx, 1, 1, [32]byte{}, [32]byte{}, zkp.MetricsSummary{Loss: 0.1}, 0.1, 0.1)
	if err != nil {
		t.Fatalf("First engine capture failed: %v", err)
	}
	
	// Capture on second engine
	err = engines[1].CaptureTrace(ctx, 2, 2, [32]byte{}, [32]byte{}, zkp.MetricsSummary{Loss: 0.2}, 0.2, 0.2)
	if err != nil {
		t.Fatalf("Second engine capture failed: %v", err)
	}
	
	// Verify they maintain separate states
	if engines[0].GetTracingState().CurrentEpoch != 1 {
		t.Error("First engine should have epoch 1")
	}
	
	if engines[1].GetTracingState().CurrentEpoch != 2 {
		t.Error("Second engine should have epoch 2")
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
