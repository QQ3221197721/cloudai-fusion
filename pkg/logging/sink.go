package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ============================================================================
// Loki Push Sink — sends structured logs to Grafana Loki
// ============================================================================

// LokiSinkConfig configures the Loki log sink.
type LokiSinkConfig struct {
	// Endpoint is the Loki push API URL (e.g., "http://loki:3100/loki/api/v1/push").
	Endpoint string

	// Labels are static labels applied to all log streams.
	Labels map[string]string

	// BatchSize is the number of log entries to batch before sending.
	BatchSize int

	// FlushInterval is the maximum time to wait before flushing a batch.
	FlushInterval time.Duration

	// Timeout for HTTP push requests.
	Timeout time.Duration
}

// LokiSink implements io.Writer and pushes logs to Grafana Loki.
type LokiSink struct {
	config LokiSinkConfig
	client *http.Client
	buffer []lokiEntry
	mu     sync.Mutex
	done   chan struct{}
	labels string // pre-formatted label string
}

type lokiEntry struct {
	Timestamp time.Time
	Line      string
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

// NewLokiSink creates a new Loki log sink.
func NewLokiSink(cfg LokiSinkConfig) *LokiSink {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{"app": "cloudai-fusion"}
	}

	sink := &LokiSink{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
		buffer: make([]lokiEntry, 0, cfg.BatchSize),
		done:   make(chan struct{}),
	}

	// Start background flusher
	go sink.flushLoop()

	return sink
}

// Write implements io.Writer — logs are buffered and pushed to Loki in batches.
func (s *LokiSink) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	s.buffer = append(s.buffer, lokiEntry{
		Timestamp: time.Now(),
		Line:      string(p),
	})
	shouldFlush := len(s.buffer) >= s.config.BatchSize
	s.mu.Unlock()

	if shouldFlush {
		go s.flush()
	}

	return len(p), nil
}

func (s *LokiSink) flushLoop() {
	ticker := time.NewTicker(s.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			s.flush()
			return
		case <-ticker.C:
			s.flush()
		}
	}
}

func (s *LokiSink) flush() {
	s.mu.Lock()
	if len(s.buffer) == 0 {
		s.mu.Unlock()
		return
	}
	entries := s.buffer
	s.buffer = make([]lokiEntry, 0, s.config.BatchSize)
	s.mu.Unlock()

	values := make([][]string, 0, len(entries))
	for _, e := range entries {
		ts := fmt.Sprintf("%d", e.Timestamp.UnixNano())
		values = append(values, []string{ts, e.Line})
	}

	payload := lokiPushRequest{
		Streams: []lokiStream{
			{
				Stream: s.config.Labels,
				Values: values,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// Close flushes remaining entries and stops the background loop.
func (s *LokiSink) Close() error {
	close(s.done)
	return nil
}

// ============================================================================
// File Rotation Sink — writes logs to a file with size-based rotation
// ============================================================================

// FileRotationConfig configures file-based log rotation.
type FileRotationConfig struct {
	// FilePath is the path to the log file.
	FilePath string

	// MaxSizeMB is the maximum size in MB before rotation.
	MaxSizeMB int

	// MaxBackups is the number of old log files to keep.
	MaxBackups int

	// MaxAgeDays is the maximum age in days before old files are deleted.
	MaxAgeDays int
}

// FileRotationSink implements io.Writer with size-based log rotation.
type FileRotationSink struct {
	config      FileRotationConfig
	file        *os.File
	currentSize int64
	mu          sync.Mutex
}

// NewFileRotationSink creates a file rotation sink.
func NewFileRotationSink(cfg FileRotationConfig) (*FileRotationSink, error) {
	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 100
	}
	if cfg.MaxBackups <= 0 {
		cfg.MaxBackups = 5
	}
	if cfg.MaxAgeDays <= 0 {
		cfg.MaxAgeDays = 30
	}

	// Ensure directory exists
	dir := filepath.Dir(cfg.FilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %w", dir, err)
	}

	sink := &FileRotationSink{config: cfg}
	if err := sink.openFile(); err != nil {
		return nil, err
	}
	return sink, nil
}

func (s *FileRotationSink) openFile() error {
	f, err := os.OpenFile(s.config.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", s.config.FilePath, err)
	}
	info, _ := f.Stat()
	s.file = f
	if info != nil {
		s.currentSize = info.Size()
	}
	return nil
}

// Write implements io.Writer with automatic rotation.
func (s *FileRotationSink) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxBytes := int64(s.config.MaxSizeMB) * 1024 * 1024
	if s.currentSize+int64(len(p)) > maxBytes {
		if err := s.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = s.file.Write(p)
	s.currentSize += int64(n)
	return n, err
}

func (s *FileRotationSink) rotate() error {
	s.file.Close()

	// Rename current file with timestamp
	ts := time.Now().Format("20060102-150405")
	ext := filepath.Ext(s.config.FilePath)
	base := s.config.FilePath[:len(s.config.FilePath)-len(ext)]
	backupName := fmt.Sprintf("%s-%s%s", base, ts, ext)
	os.Rename(s.config.FilePath, backupName)

	// Clean old backups
	s.cleanOldBackups()

	// Open new file
	return s.openFile()
}

func (s *FileRotationSink) cleanOldBackups() {
	dir := filepath.Dir(s.config.FilePath)
	baseName := filepath.Base(s.config.FilePath)
	ext := filepath.Ext(baseName)
	prefix := baseName[:len(baseName)-len(ext)]

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var backups []os.DirEntry
	for _, e := range entries {
		if e.Name() != baseName && len(e.Name()) > len(prefix) && e.Name()[:len(prefix)] == prefix {
			backups = append(backups, e)
		}
	}

	// Remove oldest if exceeding max backups
	if len(backups) > s.config.MaxBackups {
		toRemove := len(backups) - s.config.MaxBackups
		for i := 0; i < toRemove; i++ {
			os.Remove(filepath.Join(dir, backups[i].Name()))
		}
	}
}

// Close closes the file.
func (s *FileRotationSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// ============================================================================
// Multi-Sink Builder — convenience for constructing sink pipelines
// ============================================================================

// SinkBuilder helps construct a multi-sink output pipeline.
type SinkBuilder struct {
	writers []io.Writer
	closers []io.Closer
}

// NewSinkBuilder creates a new sink builder.
func NewSinkBuilder() *SinkBuilder {
	return &SinkBuilder{}
}

// WithStdout adds stdout as a sink.
func (b *SinkBuilder) WithStdout() *SinkBuilder {
	b.writers = append(b.writers, os.Stdout)
	return b
}

// WithLoki adds a Loki push sink.
func (b *SinkBuilder) WithLoki(cfg LokiSinkConfig) *SinkBuilder {
	sink := NewLokiSink(cfg)
	b.writers = append(b.writers, sink)
	b.closers = append(b.closers, sink)
	return b
}

// WithFileRotation adds a rotating file sink.
func (b *SinkBuilder) WithFileRotation(cfg FileRotationConfig) *SinkBuilder {
	sink, err := NewFileRotationSink(cfg)
	if err != nil {
		// Fall back silently — don't break if file sink fails
		return b
	}
	b.writers = append(b.writers, sink)
	b.closers = append(b.closers, sink)
	return b
}

// WithWriter adds a custom io.Writer as a sink.
func (b *SinkBuilder) WithWriter(w io.Writer) *SinkBuilder {
	b.writers = append(b.writers, w)
	return b
}

// Build returns a combined io.Writer that fans out to all sinks.
func (b *SinkBuilder) Build() io.Writer {
	if len(b.writers) == 0 {
		return os.Stdout
	}
	if len(b.writers) == 1 {
		return b.writers[0]
	}
	return io.MultiWriter(b.writers...)
}

// Close closes all closable sinks.
func (b *SinkBuilder) Close() error {
	for _, c := range b.closers {
		c.Close()
	}
	return nil
}
