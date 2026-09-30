// Package pipeline defines the data structures and interfaces for the data pipeline system.
package pipeline

import (
	"time"

	"github.com/google/uuid"
)

// Pipeline represents a data pipeline definition with source, target, and transformation rules.
type Pipeline struct {
	ID            string                `json:"id" gorm:"primaryKey"`
	Name          string                `json:"name" gorm:"not null;size:255"`
	Description   string                `json:"description,omitempty"`
	Source        DataSource            `json:"source"`
	Target        DataSink              `json:"target"`
	Transformations []TransformationRule `json:"transformations"`
	Schedule      *ScheduleConfig       `json:"schedule,omitempty"`
	Alerts        AlertConfig           `json:"alerts"`
	Status        PipelineStatus        `json:"status" gorm:"default:'active'"`
	Metadata      map[string]string     `json:"metadata,omitempty"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

// DataSource represents the source data connector configuration.
type DataSource struct {
	Type      string `json:"type" gorm:"size:50"` // postgres, mysql, snowflake, s3, kafka, etc.
	ConnStr   string `json:"conn_str" gorm:"size:1024"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Table     string `json:"table,omitempty"`
	Query     string `json:"query,omitempty"`
}

// DataSink represents the target data connector configuration.
type DataSink struct {
	Type      string `json:"type" gorm:"size:50"`
	ConnStr   string `json:"conn_str" gorm:"size:1024"`
	Config    map[string]interface{} `json:"config,omitempty"`
	Schema    string `json:"schema,omitempty"`
	Table     string `json:"table,omitempty"`
}

// TransformationRule represents an ETL transformation rule.
type TransformationRule struct {
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Type       TransformationType       `json:"type"`
	Config     TransformationConfig     `json:"config"`
	Order      int                      `json:"order"`
	Enabled    bool                     `json:"enabled"`
}

// TransformationType enum values.
type TransformationType string

const (
	TransformFilter    TransformationType = "filter"
	TransformProject TransformationType = "project"
	TransformJoin    TransformationType = "join"
	TransformAggregate TransformationType = "aggregate"
	TransformEnrich  TransformationType = "enrich"
	TransformDerive  TransformationType = "derive"
)

// TransformationConfig is a generic config for all transformation types.
type TransformationConfig struct {
	Filter    *FilterConfig    `json:"filter,omitempty"`
	Project   *ProjectConfig   `json:"project,omitempty"`
	Join      *JoinConfig      `json:"join,omitempty"`
	Aggregate *AggregateConfig `json:"aggregate,omitempty"`
	Enrich    *EnrichConfig    `json:"enrich,omitempty"`
	Derive    *DeriveConfig    `json:"derive,omitempty"`
}

// FilterConfig for filtering rows.
type FilterConfig struct {
	Expression string `json:"expression"`
}

// ProjectConfig for column projection/selection.
type ProjectConfig struct {
	Columns []string `json:"columns"`
	Rename  map[string]string `json:"rename,omitempty"`
}

// JoinConfig for joining datasets.
type JoinConfig struct {
	Type       string `json:"type"` // inner, left, right, full
	On         string `json:"on"`
	Condition  string `json:"condition,omitempty"`
}

// AggregateConfig for aggregations.
type AggregateConfig struct {
	GroupBy      []string `json:"group_by"`
	Aggregations []struct {
		Field   string `json:"field"`
		Function string `json:"function"` // sum, avg, count, min, max
		Alias   string `json:"alias"`
	} `json:"aggregations"`
}

// EnrichConfig for enriching with external data.
type EnrichConfig struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Method string `json:"method"` // lookup, api, sql
}

// DeriveConfig for derived columns using expressions.
type DeriveConfig struct {
	Column string `json:"column"`
	Expr   string `json:"expr"`
}

// ScheduleConfig defines when and how often the pipeline runs.
type ScheduleConfig struct {
	Type       ScheduleType `json:"type"` // cron, interval, once
	CronExpr   string       `json:"cron_expr,omitempty"`
	Interval   string       `json:"interval,omitempty"` // duration string like "1h", "30m"
	StartTime  time.Time    `json:"start_time,omitempty"`
	TimeZone   string       `json:"time_zone,omitempty"`
}

type ScheduleType string

const (
	ScheduleCron     ScheduleType = "cron"
	ScheduleInterval ScheduleType = "interval"
	ScheduleOnce     ScheduleType = "once"
)

// AlertConfig defines alert notifications.
type AlertConfig struct {
	Enabled    bool     `json:"enabled"`
	OnSuccess  bool     `json:"on_success,omitempty"`
	OnError    bool     `json:"on_error"`
	Recipients []string `json:"recipients"`
	Channels   []string `json:"channels"` // email, slack, webhook
}

// PipelineStatus enum values.
type PipelineStatus string

const (
	PipelineActive    PipelineStatus = "active"
	PipelineInactive  PipelineStatus = "inactive"
	PipelineArchived  PipelineStatus = "archived"
)

// RunStage represents a stage in a pipeline run execution.
type RunStage struct {
	Name       string `json:"name"`
	StageOrder int    `json:"stage_order"`
	Status     string `json:"status"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Metrics    StageMetrics `json:"metrics,omitempty"`
	ErrorMsg   string `json:"error_message,omitempty"`
}

// StageMetrics tracks execution metrics for a stage.
type StageMetrics struct {
	RecordsRead    int64 `json:"records_read,omitempty"`
	RecordsWritten int64 `json:"records_written,omitempty"`
	BytesProcessed int64 `json:"bytes_processed,omitempty"`
	DurationMs     int64 `json:"duration_ms,omitempty"`
}

// PipelineRun represents an execution of a pipeline.
type PipelineRun struct {
	ID                  string           `json:"id" gorm:"primaryKey"`
	PipelineID          string           `json:"pipeline_id" gorm:"not null;index"`
	Status              RunStatus        `json:"status"`
	StartTime           time.Time        `json:"start_time"`
	EndTime             *time.Time       `json:"end_time,omitempty"`
	DurationMs          int64            `json:"duration_ms,omitempty"`
	RecordsProcessed    int64            `json:"records_processed,omitempty"`
	Error               string           `json:"error_message,omitempty"`
	Stages              []RunStage       `json:"stages,omitempty"`
	InputStats          RunStatistics    `json:"input_stats,omitempty"`
	OutputStats         RunStatistics    `json:"output_stats,omitempty"`
	ResourceUsage       ResourceMetrics  `json:"resource_usage,omitempty"`
	TriggeredBy         string           `json:"triggered_by"` // user_id or "system" or "api"
	TriggerType         string           `json:"trigger_type"` // scheduled, manual, api
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type RunStatus string

const (
	RunPending  RunStatus = "pending"
	RunRunning  RunStatus = "running"
	RunSuccess  RunStatus = "success"
	RunFailure  RunStatus = "failure"
	RunCancelled RunStatus = "cancelled"
)

type RunStatistics struct {
	TotalRecords int64 `json:"total_records"`
	TotalBytes   int64 `json:"total_bytes"`
	Errors       int64 `json:"errors"`
	Warnings     int64 `json:"warnings"`
}

type ResourceMetrics struct {
	CPUPercent    float64 `json:"cpu_percent,omitempty"`
	MemoryBytes   int64   `json:"memory_bytes,omitempty"`
	NetworkBytesIn  int64   `json:"network_bytes_in,omitempty"`
	NetworkBytesOut int64   `json:"network_bytes_out,omitempty"`
}

// NewPipeline creates a new Pipeline with default values.
func NewPipeline(name string, source DataSource, target DataSink) *Pipeline {
	id := uuid.New().String()
	now := time.Now()
	return &Pipeline{
		ID:          id,
		Name:        name,
		Source:      source,
		Target:      target,
		Status:      PipelineActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Alerts:      AlertConfig{Enabled: true, OnError: true},
		Transformations: make([]TransformationRule, 0),
	}
}
