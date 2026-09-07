// Package redteam provides helper interfaces and utility functions for M34 Red Team Platform.
package redteam

import (
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// LOGGER INTERFACE FOR CREATING TEST DOUBLES
// ============================================================================

// loggerInterface defines minimal logging interface for decoupling
type loggerInterface interface {
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Debug(args ...interface{})
	WithField(key string, value interface{}) *logrus.Entry
	WithFields(fields logrus.Fields) *logrus.Entry
}

// loggerAdapter wraps logrus.Logger to implement loggerInterface
type loggerAdapter struct {
	logger *logrus.Logger
}

func newLoggerAdapter() loggerAdapter {
	return loggerAdapter{
		logger: logrus.New(),
	}
}

func (la loggerAdapter) Info(args ...interface{}) {
	la.logger.Info(args...)
}

func (la loggerAdapter) Warn(args ...interface{}) {
	la.logger.Warn(args...)
}

func (la loggerAdapter) Error(args ...interface{}) {
	la.logger.Error(args...)
}

func (la loggerAdapter) Debug(args ...interface{}) {
	la.logger.Debug(args...)
}

func (la loggerAdapter) WithField(key string, value interface{}) *logrus.Entry {
	return la.logger.WithField(key, value)
}

func (la loggerAdapter) WithFields(fields logrus.Fields) *logrus.Entry {
	return la.logger.WithFields(fields)
}

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

// interpolate maps value from one range to another
func interpolate(value, inMin, inMax, outMin, outMax float64) float64 {
	return (value-inMin)*(outMax-outMin)/(inMax-inMin) + outMin
}

// normalizeVector scales vector to unit length
func normalizeVector(vector []float64) []float64 {
	magnitude := 0.0
	for _, v := range vector {
		magnitude += v * v
	}
	
	magnitude = math.Sqrt(magnitude)
	if magnitude == 0 {
		return vector
	}
	
	result := make([]float64, len(vector))
	for i, v := range vector {
		result[i] = v / magnitude
	}
	
	return result
}

// dotProduct calculates inner product of two vectors
func dotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		panic("vectors must have same length")
	}
	
	result := 0.0
	for i := range a {
		result += a[i] * b[i]
	}
	
	return result
}

// formatDuration formats duration with appropriate precision
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	} else if d < time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	} else if d < time.Minute {
		return fmt.Sprintf("%.1fm %ds", d.Minutes(), d.Seconds()%60)
	}
	return d.String()
}
