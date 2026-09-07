// Package workorder provides a multi-level approval workflow system for red team operations.
package workorder

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// =========================
// Core Enums & Constants
// =========================

const (
	// Approval Levels
	LevelProjectManager = 1 // Technical feasibility review
	LevelSecurityOfficer = 2 // Compliance validation
	LevelCEO            = 3 // Legal authorization final approval

	// Work Order States
	StateDraft       = "draft"
	StateSubmitted   = "submitted"
	StatePMReview    = "pm_review"
	StateSecurityRev = "security_review"
	StateCEOApproval = "ceo_approval"
	StateApproved    = "approved"
	StateRejected    = "rejected"
	StateExpired     = "expired"

	// Notification Events
	EventWorkOrderSubmitted   = "work_order_submitted"
	EventApprovalGranted      = "approval_granted"
	EventApprovalRejected     = "approval_rejected"
	EventExpirationWarning    = "expiration_warning"
	EventEmergencyAbort       = "emergency_abort_triggered"

	// Validity Settings
	ValidityPeriodDays     = 7
)

var (
	ReminderDaysThresholds = []int{5, 6, 7}
)

const (
	// Rate Limiting
	DefaultRateLimit          = 10
	DefaultBurstConcurrency   = 3
	CPUThrottleThreshold      = 80.0
	GracefulShutdownTimeout   = 30 * time.Second
)

var (
	ErrInvalidWorkflowTransition  = errors.New("invalid workflow transition")
	ErrUnauthorizedAction         = errors.New("unauthorized action")
	ErrWorkOrderNotFound          = errors.New("work order not found")
	ErrApprovalExpired            = errors.New("approval has expired")
	ErrMissingRequiredStep        = errors.New("missing required approval step")
	ErrInvalidRequester           = errors.New("requester is not authorized")
	ErrInvalidTarget              = errors.New("target outside authorized scope")
	ErrRateLimited                = errors.New("rate limit exceeded")
	ErrEmergencyAbortFailed       = errors.New("emergency abort failed")
	ErrTimeWindowViolation        = errors.New("operation outside business hours")
	ErrHolidayBlocking            = errors.New("operation on holiday")
	ErrWeekendBlackout            = errors.New("weekend operations disabled")
)

// =========================
// Data Models
// =========================

type ApprovalLevel struct {
	ID          int
	Name        string
	Description string
	RequiredFor []string
}

type WorkOrder struct {
	ID              uuid.UUID `json:"id"`
	RequesterID     uuid.UUID `json:"requester_id"`
	RequesterName   string    `json:"requester_name"`
	RequesterEmail  string    `json:"requester_email"`
	CompanyName     string    `json:"company_name"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	OperationType   string    `json:"operation_type"`
	Targets         []string  `json:"targets"`
	Scope           ScopeDef  `json:"scope"`
	RiskLevel       string    `json:"risk_level"`
	Priority        int       `json:"priority"`
	Justification   string    `json:"justification"`

	CurrentLevel    int                `json:"current_level"`
	WorkflowState   string             `json:"workflow_state"`
	SubmissionTime  time.Time          `json:"submission_time"`
	ApprovedAt      *time.Time         `json:"approved_at,omitempty"`
	ExpiresAt       *time.Time         `json:"expires_at,omitempty"`

	ApprovalHistory []ApprovalAction `json:"approval_history"`
	RejectionReason string           `json:"rejection_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ScopeDef struct {
	AuthorizedIPs     []string    `json:"authorized_ips"`
	AuthorizedDomains []string    `json:"authorized_domains"`
	TimeWindows       TimeWindow  `json:"time_windows"`
	ExcludedTargets   []string    `json:"excluded_targets"`
	MaxConcurrentOps  int         `json:"max_concurrent_ops"`
	DataAccessLevel   string      `json:"data_access_level"`
}

type TimeWindow struct {
	StartTime       string   `json:"start_time"`
	EndTime         string   `json:"end_time"`
	Timezone        string   `json:"timezone"`
	AllowedDays     []string `json:"allowed_days"`
	HolidayCalendar []string `json:"holiday_calendar,omitempty"`
}

type ApprovalAction struct {
	Level         int       `json:"level"`
	ActorID       uuid.UUID `json:"actor_id"`
	ActorName     string    `json:"actor_name"`
	Action        string    `json:"action"`
	Timestamp     time.Time `json:"timestamp"`
	Comments      string    `json:"comments,omitempty"`
	SignatureHash string    `json:"signature_hash"`
}

type NotificationConfig struct {
	Enabled           bool            `json:"enabled"`
	Provider          string          `json:"provider"`
	SMTPConfig        SMTPConfig      `json:"smtp_config,omitempty"`
	SlackWebhook      string          `json:"slack_webhook,omitempty"`
	EmailRecipients   []string        `json:"email_recipients,omitempty"`
	SlackChannels     []string        `json:"slack_channels,omitempty"`
	UrgencyLevel      string          `json:"urgency_level"`
}

type SMTPConfig struct {
	Server      string `json:"server"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	UseTLS      bool   `json:"use_tls"`
	FROMAddress string `json:"from_address"`
}

type NotificationEvent struct {
	Type      string
	WorkOrder *WorkOrder
	Message   string
	Recipient interface{}
}

type EmailMessage struct {
	To      string
	Subject string
	Body    string
}

// =========================
// Engine Core
// =========================

type ApprovalEngine struct {
	store              WorkOrderStore
	config             EngineConfig
	mu                 sync.RWMutex
	rateLimiter        *SlidingWindowRateLimiter
	emergencyAbortChan chan struct{}
	notificationMgr    *NotificationManager
	cryptoChain        *MerkleChain
	cancelFuncs        map[uuid.UUID]context.CancelFunc
}

type EngineConfig struct {
	BaseURL              string
	ValidityDays         int
	DefaultRateLimit     int
	BurstConcurrency     int
	CPUThrottleThreshold float64
	ShutdownTimeout      time.Duration
	EnableNotifications  bool
	NotificationConfig   NotificationConfig
	DB                   WorkOrderStore
	Logger               *log.Logger
}

type WorkOrderStore interface {
	Create(ctx context.Context, wo *WorkOrder) error
	GetByID(ctx context.Context, id uuid.UUID) (*WorkOrder, error)
	Update(ctx context.Context, wo *WorkOrder) error
	List(ctx context.Context, opts ListOptions) ([]*WorkOrder, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, state string) error
}

type ListOptions struct {
	Status    string
	Limit     int
	Offset    int
	OrderBy   string
	Direction string
}

func NewApprovalEngine(cfg EngineConfig) *ApprovalEngine {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[ApprovalEngine] ", log.LstdFlags|log.Lshortfile)
	}

	engine := &ApprovalEngine{
		store:              cfg.DB,
		config:             cfg,
		rateLimiter:        NewSlidingWindowRateLimiter(DefaultRateLimit),
		emergencyAbortChan: make(chan struct{}, 1),
		notificationMgr:    NewNotificationManager(cfg.NotificationConfig),
		cryptoChain:        NewMerkleChain(),
		cancelFuncs:        make(map[uuid.UUID]context.CancelFunc),
	}

	engine.rateLimiter.SetLimit(cfg.DefaultRateLimit)
	engine.rateLimiter.SetBurst(cfg.BurstConcurrency)

	return engine
}

func (e *ApprovalEngine) Logger() *log.Logger {
	return e.config.Logger
}

func (e *ApprovalEngine) CreateWorkOrder(ctx context.Context, wo *WorkOrder) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := wo.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	wo.SubmissionTime = time.Now()
	wo.WorkflowState = StateDraft
	wo.CurrentLevel = 0
	wo.CreatedAt = time.Now()
	wo.UpdatedAt = time.Now()

	if wo.isReadyForSubmission() {
		wo.WorkflowState = StateSubmitted
		wo.CurrentLevel = LevelProjectManager
	}

	if err := e.store.Create(ctx, wo); err != nil {
		return fmt.Errorf("persist failed: %w", err)
	}

	e.Logger().Printf("Created work order ID=%s state=%s", wo.ID, wo.WorkflowState)
	return nil
}

func (e *ApprovalEngine) GetWorkOrder(ctx context.Context, id uuid.UUID) (*WorkOrder, error) {
	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWorkOrderNotFound
		}
		return nil, fmt.Errorf("retrieve failed: %w", err)
	}

	return wo, nil
}

func (e *ApprovalEngine) SubmitWorkOrder(ctx context.Context, id uuid.UUID, requesterID uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get failed: %w", err)
	}

	if wo.RequesterID != requesterID {
		return ErrUnauthorizedAction
	}

	if wo.WorkflowState != StateDraft {
		return fmt.Errorf("cannot submit in state %s", wo.WorkflowState)
	}

	wo.WorkflowState = StateSubmitted
	wo.CurrentLevel = LevelProjectManager
	wo.UpdatedAt = time.Now()

	if err := e.store.Update(ctx, wo); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	e.notificationMgr.SendNotification(NotificationEvent{
		Type:      EventWorkOrderSubmitted,
		WorkOrder: wo,
		Message:   fmt.Sprintf("Work order %s submitted for review", id),
	})

	e.Logger().Printf("Submitted work order ID=%s", id)
	return nil
}

func (e *ApprovalEngine) ApproveLevel(ctx context.Context, id uuid.UUID, actorID uuid.UUID, actorName string, comments string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get failed: %w", err)
	}

	if !e.hasPermission(actorID, wo.OperationType) {
		return ErrUnauthorizedAction
	}

	if wo.CurrentLevel == 0 || wo.CurrentLevel > 3 {
		return fmt.Errorf("invalid workflow state")
	}

	action := ApprovalAction{
		Level:       wo.CurrentLevel,
		ActorID:     actorID,
		ActorName:   actorName,
		Action:      "approved",
		Timestamp:   time.Now(),
		Comments:    comments,
	}
	action.SignatureHash = e.computeSignatureHash(wo, action)
	wo.ApprovalHistory = append(wo.ApprovalHistory, action)

	wo.CurrentLevel++
	if wo.CurrentLevel > LevelCEO {
		wo.WorkflowState = StateApproved
		now := time.Now()
		wo.ApprovedAt = &now
		expireTime := now.Add(time.Duration(e.config.ValidityDays) * 24 * time.Hour)
		wo.ExpiresAt = &expireTime
	} else {
		switch wo.CurrentLevel {
		case LevelProjectManager:
			wo.WorkflowState = StatePMReview
		case LevelSecurityOfficer:
			wo.WorkflowState = StateSecurityRev
		case LevelCEO:
			wo.WorkflowState = StateCEOApproval
		}
	}

	wo.UpdatedAt = time.Now()

	if err := e.store.Update(ctx, wo); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	nextApprover := e.getNextApprover(wo.CurrentLevel)
	e.notificationMgr.SendNotification(NotificationEvent{
		Type:      EventApprovalGranted,
		WorkOrder: wo,
		Message:   fmt.Sprintf("Approval granted by %s, proceeding to level %d", actorName, wo.CurrentLevel),
		Recipient: nextApprover,
	})

	e.Logger().Printf("Approved work order ID=%s level=%d", id, wo.CurrentLevel-1)
	return nil
}

func (e *ApprovalEngine) RejectWorkOrder(ctx context.Context, id uuid.UUID, actorID uuid.UUID, reason string, actorName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get failed: %w", err)
	}

	action := ApprovalAction{
		Level:       wo.CurrentLevel,
		ActorID:     actorID,
		ActorName:   actorName,
		Action:      "rejected",
		Timestamp:   time.Now(),
		Comments:    reason,
	}
	action.SignatureHash = e.computeSignatureHash(wo, action)
	wo.ApprovalHistory = append(wo.ApprovalHistory, action)
	wo.RejectionReason = reason
	wo.WorkflowState = StateRejected

	if err := e.store.Update(ctx, wo); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	e.notificationMgr.SendNotification(NotificationEvent{
		Type:      EventApprovalRejected,
		WorkOrder: wo,
		Message:   fmt.Sprintf("Work order rejected: %s", reason),
		Recipient: wo.RequesterID,
	})

	e.Logger().Printf("Rejected work order ID=%s reason=%s", id, reason)
	return nil
}

func (e *ApprovalEngine) CancelWorkOrder(ctx context.Context, id uuid.UUID, requesterID uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get failed: %w", err)
	}

	if wo.RequesterID != requesterID {
		return ErrUnauthorizedAction
	}

	if wo.WorkflowState != StateDraft {
		return fmt.Errorf("only draft work orders can be cancelled")
	}

	wo.WorkflowState = StateRejected
	wo.RejectionReason = "Cancelled by requester"

	if err := e.store.Update(ctx, wo); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	e.Logger().Printf("Cancelled work order ID=%s", id)
	return nil
}

func (e *ApprovalEngine) ExpireWorkOrder(ctx context.Context, id uuid.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	wo, err := e.store.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get failed: %w", err)
	}

	if wo.ExpiresAt != nil && time.Now().After(*wo.ExpiresAt) {
		wo.WorkflowState = StateExpired
		if err := e.store.Update(ctx, wo); err != nil {
			return fmt.Errorf("update failed: %w", err)
		}

		e.notificationMgr.SendNotification(NotificationEvent{
			Type:      EventExpirationWarning,
			WorkOrder: wo,
			Message:   fmt.Sprintf("Work order %s has expired", id),
		})

		e.Logger().Printf("Expired work order ID=%s", id)
	}

	return nil
}

func (w *WorkOrder) Validate() error {
	if w.Title == "" {
		return errors.New("title is required")
	}
	if len(w.Targets) == 0 {
		return errors.New("at least one target is required")
	}
	if w.RiskLevel != "low" && w.RiskLevel != "medium" && w.RiskLevel != "high" && w.RiskLevel != "critical" {
		return errors.New("invalid risk level")
	}
	if w.Priority < 1 || w.Priority > 4 {
		return errors.New("priority must be between 1 and 4")
	}
	return nil
}

func (w *WorkOrder) isReadyForSubmission() bool {
	return w.Title != "" && len(w.Targets) > 0 && w.Justification != "" && w.RiskLevel != ""
}

func (e *ApprovalEngine) hasPermission(actorID uuid.UUID, opType string) bool {
	return true
}

func (e *ApprovalEngine) getNextApprover(level int) interface{} {
	role := ""
	switch level {
	case LevelProjectManager:
		role = "project_manager"
	case LevelSecurityOfficer:
		role = "security_officer"
	case LevelCEO:
		role = "ceo"
	}
	return role
}

func (e *ApprovalEngine) computeSignatureHash(wo *WorkOrder, action ApprovalAction) string {
	data := fmt.Sprintf(
		"%s|%s|%d|%s|%s|%d",
		wo.ID.String(),
		action.ActorID.String(),
		action.Level,
		action.Action,
		action.Timestamp.UTC().Format(time.RFC3339),
		action.Level,
	)

	h := hmac.New(sha256.New, []byte("approval-signature-secret"))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func (e *ApprovalEngine) StartAutoExpirationMonitor(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				e.checkExpirations(ctx)
			}
		}
	}()
}

func (e *ApprovalEngine) checkExpirations(ctx context.Context) {
	opts := ListOptions{Limit: 100}
	workOrders, err := e.store.List(ctx, opts)
	if err != nil {
		e.Logger().Printf("List failed: %v", err)
		return
	}

	now := time.Now()
	for _, wo := range workOrders {
		if wo.ExpiresAt != nil && now.After(*wo.ExpiresAt) {
			if err := e.ExpireWorkOrder(ctx, wo.ID); err != nil {
				e.Logger().Printf("Expire failed ID=%s: %v", wo.ID, err)
			}
		}
	}
}

func (e *ApprovalEngine) SendExpirationWarnings(ctx context.Context) {
	opts := ListOptions{Limit: 100}
	workOrders, err := e.store.List(ctx, opts)
	if err != nil {
		e.Logger().Printf("List failed: %v", err)
		return
	}

	for _, wo := range workOrders {
		if wo.ExpiresAt == nil {
			continue
		}

		daysLeft := int(time.Until(*wo.ExpiresAt).Hours() / 24)
		for _, threshold := range ReminderDaysThresholds {
			if daysLeft == threshold {
				e.sendReminderEmail(wo, daysLeft)
			}
		}
	}
}

func (e *ApprovalEngine) sendReminderEmail(wo *WorkOrder, daysLeft int) {
	subject := fmt.Sprintf("URGENT: Work Order %s expires in %d day(s)", wo.ID, daysLeft)
	body := fmt.Sprintf(`
Dear %s,

Your work order "%s" (ID: %s) will expire in %d day(s) on %s.

Please take appropriate action before expiration.

Best regards,
Red Team Platform
`, wo.RequesterName, wo.Title, wo.ID, daysLeft, wo.ExpiresAt.Format("January 2, 2006"))

	email := EmailMessage{
		To:      wo.RequesterEmail,
		Subject: subject,
		Body:    body,
	}

	if err := e.notificationMgr.SendEmail(email); err != nil {
		e.Logger().Printf("Send reminder failed: %v", err)
	}
}

func (e *ApprovalEngine) EmergencyAbort(ctx context.Context) error {
	select {
	case e.emergencyAbortChan <- struct{}{}:
		e.Logger().Print("EMERGENCY ABORT triggered")

		e.mu.Lock()
		for id, cancel := range e.cancelFuncs {
			cancel()
			delete(e.cancelFuncs, id)
			e.Logger().Printf("Cancelled operation ID=%s", id)
		}
		e.mu.Unlock()

		e.notificationMgr.SendNotification(NotificationEvent{
			Type:      EventEmergencyAbort,
			Message:   "Emergency abort executed - all operations terminated",
		})

		return nil
	default:
		return ErrEmergencyAbortFailed
	}
}

func (e *ApprovalEngine) SendSlackAlert(message string) error {
	if e.config.NotificationConfig.SlackWebhook == "" {
		return errors.New("Slack webhook URL not configured")
	}

	webhook := e.config.NotificationConfig.SlackWebhook
	payload := map[string]string{
		"text": message,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	resp, err := http.Post(webhook, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("post failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code: %d", resp.StatusCode)
	}

	return nil
}

// =========================
// Helper Types & Structures
// =========================

type SlidingWindowRateLimiter struct {
	limit      int
	burst      int
	window     time.Duration
	requests   map[string][]time.Time
	mu         sync.RWMutex
}

type MerkleTree struct {
	hashes [][]byte
	depth  int
}

type MerkleChain struct {
	lastHash []byte
}

func NewSlidingWindowRateLimiter(limit int) *SlidingWindowRateLimiter {
	return &SlidingWindowRateLimiter{
		limit:    limit,
		burst:    3,
		window:   time.Minute,
		requests: make(map[string][]time.Time),
	}
}

func (r *SlidingWindowRateLimiter) SetLimit(l int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limit = l
}

func (r *SlidingWindowRateLimiter) SetBurst(b int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.burst = b
}

func (r *SlidingWindowRateLimiter) Allow(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-r.window)

	var validRequests []time.Time
	for _, t := range r.requests[clientID] {
		if t.After(windowStart) {
			validRequests = append(validRequests, t)
		}
	}

	if len(validRequests) >= r.limit {
		return false
	}

	r.requests[clientID] = append(validRequests, now)
	return true
}

func NewMerkleChain() *MerkleChain {
	return &MerkleChain{lastHash: []byte("genesis")}
}

func (m *MerkleChain) AddBlock(data []byte) []byte {
	h := sha256.Sum256(append(data, m.lastHash...))
	m.lastHash = h[:]
	return h[:]
}

func (m *MerkleChain) GetLastHash() []byte {
	return m.lastHash
}

type NotificationManager struct {
	config  NotificationConfig
	smtpClient *SMTPClient
}

func NewNotificationManager(cfg NotificationConfig) *NotificationManager {
	nm := &NotificationManager{
		config: cfg,
	}
	if cfg.SMTPConfig.Server != "" {
		nm.smtpClient = &SMTPClient{config: cfg.SMTPConfig}
	}
	return nm
}

func (nm *NotificationManager) SendNotification(event NotificationEvent) error {
	if !nm.config.Enabled {
		return nil
	}

	switch nm.config.Provider {
	case "smtp":
		return nm.sendEmail(event)
	case "slack":
		return nm.sendSlack(event)
	default:
		return errors.New("unknown notification provider")
	}
}

func (nm *NotificationManager) SendEmail(email EmailMessage) error {
	if nm.smtpClient == nil {
		return errors.New("SMTP client not configured")
	}
	return nm.smtpClient.Send(email.To, email.Subject, email.Body)
}

type SMTPClient struct {
	config SMTPConfig
}

func (c *SMTPClient) Send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", c.config.Server, c.config.Port)
	auth := smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.Server)

	headers := map[string]string{
		"From": c.config.FROMAddress,
		"To":   to,
		"Subject": subject,
		"Content-Type": "text/plain; charset=UTF-8",
	}

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body

	err := smtp.SendMail(addr, auth, c.config.FROMAddress, []string{to}, []byte(message))
	if err != nil {
		return fmt.Errorf("send failed: %w", err)
	}
	return nil
}

func (nm *NotificationManager) sendEmail(event NotificationEvent) error {
	body := fmt.Sprintf("%s\n\n%s", event.WorkOrder.Title, event.Message)
	email := EmailMessage{
		To:      "approver@example.com",
		Subject: fmt.Sprintf("[Approval Required] %s", event.WorkOrder.Title),
		Body:    body,
	}
	return nm.smtpClient.Send(email.To, email.Subject, email.Body)
}

func (nm *NotificationManager) sendSlack(event NotificationEvent) error {
	webhook := nm.config.SlackWebhook
	payload := map[string]interface{}{
		"text": fmt.Sprintf("⚠️ %s\n\n%s", event.WorkOrder.Title, event.Message),
	}

	jsonData, _ := json.Marshal(payload)
	_, err := http.Post(webhook, "application/json", bytes.NewReader(jsonData))
	return err
}
