package training

import (
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestTrialManager_BasicOperations(t *testing.T) {
	cfg := TrialManagerConfig{
		MaxParallelTrials: 5,
		EvidenceLedger:    &evidence.NopRecorder{},
	}
	manager := NewTrialManager(cfg)

	t.Run("SubmitAndCompleteTrial", func(t *testing.T) {
		params := map[string]float64{
			"learning_rate": 0.001,
			"batch_size":    32,
		}

		trial, err := manager.SubmitTrial("job-1", params)
		if err != nil {
			t.Fatalf("Failed to submit trial: %v", err)
		}

		if trial == nil {
			t.Fatal("Trial should not be nil")
		}

		if trial.Status != TrialPending {
			t.Errorf("Expected status pending, got %s", trial.Status.String())
		}

		metrics := map[string]float64{
			"loss":      0.5,
			"accuracy":  0.85,
		}

		err = manager.CompleteTrial(trial.ID, metrics, TrialSuccess)
		if err != nil {
			t.Fatalf("Failed to complete trial: %v", err)
		}

		retrieved, err := manager.GetTrial(trial.ID)
		if err != nil {
			t.Fatalf("Failed to retrieve trial: %v", err)
		}

		if retrieved.Status != TrialSuccess {
			t.Errorf("Expected status success, got %s", retrieved.Status.String())
		}
	})

	t.Run("GetBestParameters", func(t *testing.T) {
		bestParams, err := manager.GetBestParameters()
		if err != nil {
			t.Fatalf("GetBestParameters failed: %v", err)
		}

		if bestParams["learning_rate"] != 0.001 {
			t.Errorf("Expected learning_rate 0.001, got %f", bestParams["learning_rate"])
		}
	})

	t.Run("GetBestLoss", func(t *testing.T) {
		loss, found := manager.GetBestLoss()
		if !found {
			t.Fatal("Best loss should be found")
		}

		if loss != 0.5 {
			t.Errorf("Expected loss 0.5, got %f", loss)
		}
	})
}

func TestTrialManager_EarlyStopping(t *testing.T) {
	cfg := TrialManagerConfig{
		MaxParallelTrials:        1,
		EvidenceLedger:           &evidence.NopRecorder{},
		MinTrialsToSkipEarlyStopping: 3,
	}
	manager := NewTrialManager(cfg)

	params := map[string]float64{
		"learning_rate": 0.001,
	}

	trial, _ := manager.SubmitTrial("job-early", params)
	manager.RecordProgress(trial.ID, 1, map[string]float64{"loss": 1.0})
	manager.RecordProgress(trial.ID, 2, map[string]float64{"loss": 0.9})
	manager.RecordProgress(trial.ID, 3, map[string]float64{"loss": 0.85})

	stop, reason := manager.ShouldApplyEarlyStopping(trial.ID)
	if stop {
		t.Logf("Early stopping triggered: %s", reason)
	} else {
		t.Log("Early stopping not triggered yet (expected)")
	}
}

func TestTrialManager_MaxParallelLimit(t *testing.T) {
	cfg := TrialManagerConfig{
		MaxParallelTrials: 2,
		EvidenceLedger:    &evidence.NopRecorder{},
	}
	manager := NewTrialManager(cfg)

	params := map[string]float64{"learning_rate": 0.001}

	trial1, _ := manager.SubmitTrial("job-limit", params)
	trial2, _ := manager.SubmitTrial("job-limit", params)

	trial3, err := manager.SubmitTrial("job-limit", params)
	if err == nil {
		t.Fatal("Expected error for exceeding max parallel trials, got nil")
	}

	if trial3 != nil {
		t.Errorf("Expected nil trial, got %v", trial3)
	}

	_ = trial1
	_ = trial2
}

func TestGenerateTrialID(t *testing.T) {
	jobID := "test-job-123"
	id1 := generateTrialID(jobID, 1)
	id2 := generateTrialID(jobID, 2)

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}

	if len(id1) != 16 {
		t.Errorf("Expected ID length 16, got %d", len(id1))
	}

	if len(id2) != 16 {
		t.Errorf("Expected ID length 16, got %d", len(id2))
	}
}

func TestBayesianOptimizer_SuggestParameters(t *testing.T) {
	optimizer := NewBayesianOptimizer()

	historicalTrials := []Trial{
		{
			ID:         "trial-1",
			JobID:      "job-opt",
			Parameters: map[string]float64{"lr": 0.001, "bs": 32},
			Metrics:    map[string]float64{"loss": 0.5, "acc": 0.85},
			Status:     TrialSuccess,
			StartedAt:  time.Now().UTC(),
		},
		{
			ID:         "trial-2",
			JobID:      "job-opt",
			Parameters: map[string]float64{"lr": 0.01, "bs": 64},
			Metrics:    map[string]float64{"loss": 0.3, "acc": 0.90},
			Status:     TrialSuccess,
			StartedAt:  time.Now().UTC(),
		},
	}

	suggested, err := optimizer.SuggestParameters("job-opt", historicalTrials)
	if err != nil {
		t.Fatalf("SuggestParameters failed: %v", err)
	}

	if len(suggested) == 0 {
		t.Error("Suggested parameters should not be empty")
	}
}
