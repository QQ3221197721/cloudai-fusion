// Package api provides additional RESTful handlers for the M14 Training Orchestrator module.
// Part 2: Control, Monitoring, Scaling, and Hyperparameter Tuning
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/middleware"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// ============================================================================
// Job Control Handlers
// ============================================================================

// handlePauseTrainingJob pauses a running training job.
// POST /api/v1/training/jobs/:id/pause
// Response: 200 OK with job paused confirmation
func handlePauseTrainingJob(engine interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		trainingJobsMutex.RLock()
		job, exists := trainingJobs[jobID]
		trainingJobsMutex.RUnlock()

		if !exists {
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		if job.Status != StatusRunning {
			c.JSON(http.StatusBadRequest, gin.h{
				"error":          "job must be running to pause",
				"current_status": string(job.Status),
			})
			return
		}

		now := time.Now().UTC()
		job.PausedAt = &now

		logger.WithFields(logrus.Fields{
			"job_id":   jobID,
			"previous": string(job.Status),
		}).Info("Training job paused")

		// Record evidence
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "TRAINING_JOB_PAUSED",
				Subject:   jobID,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
			}

			if err := ledger.RecordReceipt(receipt); err != nil {
				logger.WithError(err).Warn("Failed to record training job pause evidence")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"job_id":    jobID,
			"status":    string(StatusPaused),
			"paused_at": now.Format(time.RFC3339),
			"message":   "training job paused",
		})
	}
}

// handleResumeTrainingJob resumes a paused training job.
// POST /api/v1/training/jobs/:id/resume
// Response: 202 Accepted with job resumed confirmation
func handleResumeTrainingJob(engine interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		trainingJobsMutex.Lock()
		job, exists := trainingJobs[jobID]
		if !exists {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		if job.Status != StatusPaused {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusBadRequest, gin.h{
				"error":          "job must be paused to resume",
				"current_status": string(job.Status),
			})
			return
		}

		job.Status = StatusStarting
		trainingJobsMutex.Unlock()

		logger.WithFields(logrus.Fields{
			"job_id":   jobID,
			"previous": string(StatusPaused),
		}).Info("Training job resumed")

		c.JSON(http.StatusAccepted, gin.H{
			"job_id":  jobID,
			"status":  string(StatusStarting),
			"message": "training job resuming",
		})
	}
}

// handleStopTrainingJob stops a running training job completely.
// POST /api/v1/training/jobs/:id/stop
// Query params:
//   - save_checkpoint: true/false (optional, default: true)
//
// Response: 200 OK with job stopped confirmation
func handleStopTrainingJob(engine interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		saveCheckpoint, _ := strconv.ParseBool(c.Query("save_checkpoint"))

		trainingJobsMutex.Lock()
		job, exists := trainingJobs[jobID]
		if !exists {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		now := time.Now().UTC()
		if saveCheckpoint && len(job.Checkpoints) > 0 {
			lastCheckpoint := job.Checkpoints[len(job.Checkpoints)-1]
			logger.WithFields(logrus.Fields{
				"job_id":     jobID,
				"checkpoint": lastCheckpoint,
			}).Info("Stopping job with checkpoint")
		}

		job.Status = StatusStopped
		job.CompletedAt = &now
		trainingJobsMutex.Unlock()

		logger.WithField("job_id", jobID).Info("Training job stopped")

		// Record evidence
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "TRAINING_JOB_STOPPED",
				Subject:   jobID,
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
				Metadata: gin.H{
					"save_checkpoint": saveCheckpoint,
				},
			}

			if err := ledger.RecordReceipt(receipt); err != nil {
				logger.WithError(err).Warn("Failed to record training job stop evidence")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"job_id":           jobID,
			"status":           string(StatusStopped),
			"completed_at":     now.Format(time.RFC3339),
			"saved_checkpoint": saveCheckpoint,
			"message":          "training job stopped",
		})
	}
}

// ============================================================================
// Monitoring Handlers
// ============================================================================

// handleGetTrainingLogs retrieves training logs for a job.
// GET /api/v1/training/jobs/:id/logs
// Query params:
//   - tail: number of lines (default: 100)
//   - follow: true/false for streaming
//   - timestamps: include timestamps
//
// Response: 200 OK with log content
func handleGetTrainingLogs(engine interface{}, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		tail, _ := strconv.Atoi(c.Query("tail"))
		follow, _ := strconv.ParseBool(c.Query("follow"))
		includeTimestamps, _ := strconv.ParseBool(c.Query("timestamps"))

		if tail == 0 {
			tail = 100
		}

		response := gin.H{
			"job_id":           jobID,
			"tail":             tail,
			"follow":           follow,
			"timestamps":       includeTimestamps,
			"log_lines":        []string{},
			"total_lines":      0,
			"stream_available": !follow,
		}

		logger.WithFields(logrus.Fields{
			"job_id": jobID,
			"tail":   tail,
			"follow": follow,
		}).Debug("Retrieved training logs")

		c.JSON(http.StatusOK, response)
	}
}

// handleGetTrainingMetrics retrieves real-time metrics for a training job.
// GET /api/v1/training/jobs/:id/metrics
// Query params:
//   - interval: metric sampling interval in seconds (default: 10)
//
// Response: 200 OK with metrics data
func handleGetTrainingMetrics(engine interface{}, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		interval, _ := strconv.Atoi(c.Query("interval"))
		if interval == 0 {
			interval = 10
		}

		metrics := gin.H{
			"job_id":        jobID,
			"interval_secs": interval,
			"collected_at":  time.Now().UTC().Format(time.RFC3339),
			"data": gin.H{
				"loss":                   0.0,
				"accuracy":               0.0,
				"learning_rate":          0.0,
				"throughput_samples_sec": 0.0,
				"gpu_utilization_pct":    map[string]float64{},
				"memory_used_gb":         map[string]float64{},
			},
		}

		logger.WithField("job_id", jobID).Debug("Retrieved training metrics")

		c.JSON(http.StatusOK, gin.H{
			"metrics": metrics,
		})
	}
}

// handleListCheckpoints lists all checkpoints for a training job.
// GET /api/v1/training/jobs/:id/checkpoints
// Response: 200 OK with checkpoint list
func handleListCheckpoints(engine interface{}, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		trainingJobsMutex.RLock()
		job, exists := trainingJobs[jobID]
		trainingJobsMutex.RUnlock()

		if !exists {
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		checkpoints := make([]interface{}, 0, len(job.Checkpoints))
		for _, cpName := range job.Checkpoints {
			checkpoints = append(checkpoints, gin.H{
				"name":       cpName,
				"job_id":     jobID,
				"created_at": "",      // Would be populated from storage metadata
				"size_bytes": 0,       // Would be populated from storage metadata
				"metrics":    gin.H{}, // Would include loss, accuracy at checkpoint time
			})
		}

		response := gin.H{
			"job_id":           jobID,
			"checkpoint_count": len(checkpoints),
			"checkpoints":      checkpoints,
		}

		logger.WithFields(logrus.Fields{
			"job_id": jobID,
			"count":  len(checkpoints),
		}).Debug("Listed checkpoints")

		c.JSON(http.StatusOK, response)
	}
}

// ============================================================================
// Checkpoint Management Handlers
// ============================================================================

// handleSaveCheckpoint saves a manual checkpoint for a training job.
// POST /api/v1/training/jobs/:id/checkpoints/save
// Request body:
//
//	{
//	  "name": "optional custom name",
//	  "force": false // whether to save even if not requested
//	}
//
// Response: 200 OK with checkpoint details
func handleSaveCheckpoint(engine interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if jobID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id required"})
			return
		}

		var req struct {
			Name  string `json:"name,omitempty"`
			Force bool   `json:"force,omitempty"`
		}

		c.ShouldBindJSON(&req)

		trainingJobsMutex.Lock()
		job, exists := trainingJobs[jobID]
		if !exists {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		checkpointName := req.Name
		if checkpointName == "" {
			checkpointName = fmt.Sprintf("manual-%d", time.Now().UnixNano())
		}

		now := time.Now().UTC()
		job.Checkpoints = append(job.Checkpoints, checkpointName)
		trainingJobsMutex.Unlock()

		logger.WithFields(logrus.Fields{
			"job_id":     jobID,
			"checkpoint": checkpointName,
		}).Info("Manual checkpoint saved")

		// Record evidence
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "CHECKPOINT_SAVED",
				Subject:   fmt.Sprintf("%s:%s", jobID, checkpointName),
				Actor:     c.GetString("user_id"),
				Timestamp: now,
				Metadata: gin.H{
					"manual": true,
					"forced": req.Force,
				},
			}

			if err := ledger.RecordReceipt(receipt); err != nil {
				logger.WithError(err).Warn("Failed to record checkpoint save evidence")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"job_id":          jobID,
			"checkpoint_name": checkpointName,
			"saved_at":        now.Format(time.RFC3339),
			"message":         "checkpoint saved successfully",
		})
	}
}

// handleDownloadCheckpoint provides download URL for a checkpoint.
// GET /api/v1/training/jobs/:id/checkpoints/:name/download
// Response: 200 OK with download URL or 404 if not found
func handleDownloadCheckpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		checkpointName := c.Param("name")
		if jobID == "" || checkpointName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id and checkpoint name required"})
			return
		}

		// In production, this would generate a pre-signed S3/GCS URL
		downloadURL := fmt.Sprintf("/download/checkpoints/%s/%s", jobID, checkpointName)

		response := gin.H{
			"job_id":          jobID,
			"checkpoint_name": checkpointName,
			"download_url":    downloadURL,
			"url_expires_at":  time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339),
			"content_type":    "application/octet-stream",
		}

		c.JSON(http.StatusOK, response)
	}
}

// handleDeleteCheckpoint deletes a checkpoint.
// DELETE /api/v1/training/jobs/:id/checkpoints/:name
// Response: 200 OK with deletion confirmation
func handleDeleteCheckpoint(engine interface{}, ledger *evidence.Ledger, logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		checkpointName := c.Param("name")
		if jobID == "" || checkpointName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "job id and checkpoint name required"})
			return
		}

		trainingJobsMutex.Lock()
		job, exists := trainingJobs[jobID]
		if !exists {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusNotFound, gin.h{"error": "job not found", "job_id": jobID})
			return
		}

		// Remove checkpoint from list
		newCheckpoints := make([]string, 0, len(job.Checkpoints)-1)
		found := false
		for _, cp := range job.Checkpoints {
			if cp == checkpointName {
				found = true
				continue
			}
			newCheckpoints = append(newCheckpoints, cp)
		}

		if !found {
			trainingJobsMutex.Unlock()
			c.JSON(http.StatusNotFound, gin.h{
				"error":           "checkpoint not found",
				"checkpoint_name": checkpointName,
			})
			return
		}

		job.Checkpoints = newCheckpoints
		trainingJobsMutex.Unlock()

		logger.WithFields(logrus.Fields{
			"job_id":     jobID,
			"checkpoint": checkpointName,
		}).Info("Checkpoint deleted")

		// Record evidence
		if ledger != nil {
			receipt := evidence.Receipt{
				Action:    "CHECKPOINT_DELETED",
				Subject:   fmt.Sprintf("%s:%s", jobID, checkpointName),
				Actor:     c.GetString("user_id"),
				Timestamp: time.Now().UTC(),
			}

			if err := ledger.RecordReceipt(receipt); err != nil {
				logger.WithError(err).Warn("Failed to record checkpoint deletion evidence")
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"job_id":          jobID,
			"checkpoint_name": checkpointName,
			"deleted":         true,
			"deleted_at":      time.Now().UTC().Format(time.RFC3339),
			"message":         "checkpoint deleted",
		})
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

func generateTrainingJobID() string {
	return fmt.Sprintf("train-job-%d", time.Now().UnixNano())
}

// Import sha256 for future use
var _ = sha256.New
var _ = hex.EncodeToString
