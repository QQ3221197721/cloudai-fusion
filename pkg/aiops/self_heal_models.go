// Package aiops - shared models for the ensemble self-healing engine.
// These types back the ensemble/forest self-heal implementation
// (relocated here from their original package aiops source).
package aiops

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// MetricsSnapshot represents a point in time metrics collection.
type MetricsSnapshot struct {
	Timestamp      time.Time `json:"timestamp"`
	CPUUtilization float64   `json:"cpu_util"`
	MemoryUsage    float64   `json:"memory_usage"`
	DiskIORead     float64   `json:"disk_io_read"`
	DiskIOWrite    float64   `json:"disk_io_write"`
	NetworkIn      float64   `json:"network_in"`
	NetworkOut     float64   `json:"network_out"`
	Connections    int       `json:"connections"`
	GPUUtilization float64   `json:"gpu_util,omitempty"`
	GPUMemory      float64   `json:"gpu_memory,omitempty"`
	ErrorRate      float64   `json:"error_rate"`
	LatencyP99     float64   `json:"latency_p99"`
}

// AutoEncoderModel is a placeholder for a deep-learning reconstruction model.
type AutoEncoderModel struct{}

// AnomalyDetectionEnsemble combines multiple anomaly detection models.
type AnomalyDetectionEnsemble struct {
	logger *logrus.Logger

	// Model 1: Mahalanobis distance for multivariate outlier detection
	mahalanobisModel *MahalanobisDistanceModel

	// Model 2: Isolation Forest for high-dimensional anomaly detection
	isolationForest *IsolationForestModel

	// Model 3: Autoencoder reconstruction error (placeholder for deep learning)
	autoEncoder *AutoEncoderModel

	// Ensemble weights
	weights [3]float64
}

// ============================================================================
// MAHALANOBIS DISTANCE MODEL - STATISTICAL ANOMALY DETECTION
// ============================================================================

// MahalanobisDistanceModel implements statistical outlier detection.
type MahalanobisDistanceModel struct {
	logger      *logrus.Logger
	mean        []float64
	covariance  [][]float64
	inverseCov  [][]float64 // pre-computed for efficiency
	trained     bool
	numFeatures int
}

// NewMahalanobisDistanceModel creates new model instance.
func NewMahalanobisDistanceModel(logger *logrus.Logger) *MahalanobisDistanceModel {
	return &MahalanobisDistanceModel{
		logger:      logger,
		numFeatures: 8, // CPU, Memory, Disk IOx2, Network x2, Connections, ErrorRate
		mean:        make([]float64, 8),
		covariance:  make([][]float64, 8),
		inverseCov:  make([][]float64, 8),
	}
}

// Train model on historical data.
func (m *MahalanobisDistanceModel) Train(data []MetricsSnapshot) error {
	if len(data) < 10 {
		return fmt.Errorf("insufficient training data: need at least 10 samples, got %d", len(data))
	}

	mean := m.computeMean(data)
	copy(m.mean, mean)

	covariance := m.computeCovariance(data, mean)
	m.covariance = covariance

	inverseCov, err := m.inverseMatrix(covariance)
	if err != nil {
		return fmt.Errorf("failed to invert covariance matrix: %w", err)
	}
	m.inverseCov = inverseCov

	m.trained = true
	m.logger.Info("Mahalanobis model trained successfully")
	return nil
}

// computeMean calculates mean for each feature.
func (m *MahalanobisDistanceModel) computeMean(data []MetricsSnapshot) []float64 {
	n := float64(len(data))
	mean := make([]float64, m.numFeatures)

	for _, snapshot := range data {
		mean[0] += snapshot.CPUUtilization
		mean[1] += snapshot.MemoryUsage
		mean[2] += snapshot.DiskIORead
		mean[3] += snapshot.DiskIOWrite
		mean[4] += snapshot.NetworkIn
		mean[5] += snapshot.NetworkOut
		mean[6] += float64(snapshot.Connections)
		mean[7] += snapshot.ErrorRate
	}

	for i := range mean {
		mean[i] /= n
	}

	return mean
}

// computeCovariance calculates covariance matrix.
func (m *MahalanobisDistanceModel) computeCovariance(data []MetricsSnapshot, mean []float64) [][]float64 {
	n := float64(len(data) - 1)
	covariance := make([][]float64, m.numFeatures)

	for i := range covariance {
		covariance[i] = make([]float64, m.numFeatures)
	}

	for _, snapshot := range data {
		deviations := m.getDeviations(snapshot, mean)

		for i := 0; i < m.numFeatures; i++ {
			for j := i; j < m.numFeatures; j++ {
				covariance[i][j] += deviations[i] * deviations[j] / n
				if i != j {
					covariance[j][i] = covariance[i][j]
				}
			}
		}
	}

	return covariance
}

// getDeviations returns deviation vector from mean.
func (m *MahalanobisDistanceModel) getDeviations(snapshot MetricsSnapshot, mean []float64) []float64 {
	deviations := make([]float64, m.numFeatures)

	deviations[0] = snapshot.CPUUtilization - mean[0]
	deviations[1] = snapshot.MemoryUsage - mean[1]
	deviations[2] = snapshot.DiskIORead - mean[2]
	deviations[3] = snapshot.DiskIOWrite - mean[3]
	deviations[4] = snapshot.NetworkIn - mean[4]
	deviations[5] = snapshot.NetworkOut - mean[5]
	deviations[6] = float64(snapshot.Connections) - mean[6]
	deviations[7] = snapshot.ErrorRate - mean[7]

	return deviations
}

// inverseMatrix inverts a square matrix using Gauss-Jordan elimination
// with partial pivoting. Returns an error if the matrix is singular.
func (m *MahalanobisDistanceModel) inverseMatrix(matrix [][]float64) ([][]float64, error) {
	n := len(matrix)

	// Build augmented working copy [A | I].
	a := make([][]float64, n)
	inv := make([][]float64, n)
	for i := 0; i < n; i++ {
		a[i] = make([]float64, n)
		inv[i] = make([]float64, n)
		copy(a[i], matrix[i])
		inv[i][i] = 1.0
	}

	for col := 0; col < n; col++ {
		// Partial pivoting: find the row with the largest absolute value.
		pivotRow := col
		maxVal := math.Abs(a[col][col])
		for r := col + 1; r < n; r++ {
			if v := math.Abs(a[r][col]); v > maxVal {
				maxVal = v
				pivotRow = r
			}
		}
		if maxVal < 1e-12 {
			return nil, fmt.Errorf("matrix is singular at column %d", col)
		}

		if pivotRow != col {
			a[col], a[pivotRow] = a[pivotRow], a[col]
			inv[col], inv[pivotRow] = inv[pivotRow], inv[col]
		}

		// Normalize the pivot row.
		pivot := a[col][col]
		for j := 0; j < n; j++ {
			a[col][j] /= pivot
			inv[col][j] /= pivot
		}

		// Eliminate the current column from every other row.
		for r := 0; r < n; r++ {
			if r == col {
				continue
			}
			factor := a[r][col]
			if factor == 0 {
				continue
			}
			for j := 0; j < n; j++ {
				a[r][j] -= factor * a[col][j]
				inv[r][j] -= factor * inv[col][j]
			}
		}
	}

	return inv, nil
}

// IsScore computes Mahalanobis distance squared for a feature vector.
func (m *MahalanobisDistanceModel) IsScore(x []float64) float64 {
	if !m.trained {
		return 0.0
	}

	diff := make([]float64, m.numFeatures)
	for i := range diff {
		if i < len(x) {
			diff[i] = x[i] - m.mean[i]
		}
	}

	tmp := make([]float64, m.numFeatures)
	for i := range tmp {
		sum := 0.0
		for j := range tmp {
			sum += m.inverseCov[i][j] * diff[j]
		}
		tmp[i] = sum
	}

	score := 0.0
	for i := range diff {
		score += diff[i] * tmp[i]
	}

	return score
}

// ============================================================================
// SELF-HEAL METRICS
// ============================================================================

// SelfHealMetrics tracks self-healing action counters.
type SelfHealMetrics struct {
	mu          sync.RWMutex
	ScaleUps    int64
	ScaleDowns  int64
	Restarts    int64
	Isolations  int64
	TotalEvents int64
}

// NewSelfHealMetrics creates a new metrics tracker.
func NewSelfHealMetrics() *SelfHealMetrics {
	return &SelfHealMetrics{}
}

// RecordScaleUp records a scale-up remediation.
func (m *SelfHealMetrics) RecordScaleUp() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ScaleUps++
	m.TotalEvents++
}

// RecordScaleDown records a scale-down remediation.
func (m *SelfHealMetrics) RecordScaleDown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ScaleDowns++
	m.TotalEvents++
}

// RecordRestart records a service restart remediation.
func (m *SelfHealMetrics) RecordRestart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Restarts++
	m.TotalEvents++
}

// RecordIsolation records a service isolation remediation.
func (m *SelfHealMetrics) RecordIsolation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Isolations++
	m.TotalEvents++
}
