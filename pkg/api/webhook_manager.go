package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// WebhookManager manages webhook registrations and deliveries
type WebhookManager struct {
	mu            sync.RWMutex
	webhooks      map[string]*WebhookConfig
	deliveries    map[string]*DeliveryEvent
	retryQueue    chan *DeliveryTask
	eventHandlers map[string][]EventHandler
	maxRetries    int
	baseDelay     time.Duration
	logger        WebhookLogger
	store         WebhookStore
}

// WebhookConfig defines webhook configuration
type WebhookConfig struct {
	ID          string                    `json:"id"`
	URL         string                    `json:"url"`
	Events      []string                  `json:"events"`
	Description string                    `json:"description,omitempty"`
	Secret      string                    `json:"secret,omitempty"` // For signature verification
	Headers     map[string]string         `json:"headers,omitempty"`
	Active      bool                      `json:"active"`
	CreatedAt   time.Time                 `json:"created_at"`
	Metadata    map[string]interface{}    `json:"metadata,omitempty"`
	Filters     map[string]interface{}    `json:"filters,omitempty"`
}

// DeliveryEvent tracks webhook delivery status
type DeliveryEvent struct {
	ID           string                    `json:"id"`
	WebhookID    string                    `json:"webhook_id"`
	Event        interface{}               `json:"event"`
	Status       DeliveryStatus            `json:"status"`
	RetryCount   int                       `json:"retry_count"`
	StatusCode   int                       `json:"status_code,omitempty"`
	Body         string                    `json:"body,omitempty"`
	TriedAt      []time.Time               `json:"tried_at,omitempty"`
	NextRetry    time.Time                 `json:"next_retry,omitempty"`
	LastError    string                    `json:"last_error,omitempty"`
	ResponseTime time.Duration             `json:"response_time,omitempty"`
	Attempts     []DeliveryAttempt         `json:"attempts,omitempty"`
}

// DeliveryStatus represents current delivery status
type DeliveryStatus string

const (
	DeliveryPending  DeliveryStatus = "pending"
	DeliveryRunning  DeliveryStatus = "running"
	DeliverySuccess  DeliveryStatus = "success"
	DeliveryFailed   DeliveryStatus = "failed"
	DeliveryExhausted DeliveryStatus = "exhausted"
)

// DeliveryAttempt records single delivery attempt
type DeliveryAttempt struct {
	TriedAt     time.Time `json:"tried_at"`
	StatusCode  int       `json:"status_code,omitempty"`
	DurationMs  int       `json:"duration_ms,omitempty"`
	Success     bool      `json:"success"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// DeliveryTask for async processing
type DeliveryTask struct {
	Config *WebhookConfig
	Event  interface{}
}

// EventHandler processes webhook events synchronously
type EventHandler func(event interface{}) error

// WebhookLogger for logging webhook events
type WebhookLogger interface {
	LogWebhook(id, event, level, message string)
	LogDelivery(webhookID, deliveryID, status string)
	LogError(webhookID, deliveryID, err string)
}

// DefaultLogger provides basic console logging
type DefaultLogger struct{}

func (DefaultLogger) LogWebhook(id, event, level, message string) {
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Printf("[%s] %s: %s [%s]\n", timestamp, level, id, message)
}

func (DefaultLogger) LogDelivery(webhookID, deliveryID, status string) {
	DefaultLogger{}.LogWebhook(webhookID, "", "INFO", fmt.Sprintf("Delivery %s: %s", status, deliveryID))
}

func (DefaultLogger) LogError(webhookID, deliveryID, err string) {
	DefaultLogger{}.LogWebhook(webhookID, "", "ERROR", fmt.Sprintf("Error in delivery %s: %v", deliveryID, err))
}

// WebhookStore persistence interface
type WebhookStore interface {
	Save(config *WebhookConfig) error
	Delete(id string) error
	Get(id string) (*WebhookConfig, error)
	List(activeOnly bool) ([]*WebhookConfig, error)
	SaveDelivery(delivery *DeliveryEvent) error
}

// NewWebhookManager creates webhook manager instance
func NewWebhookManager(opts ...WebhookManagerOption) *WebhookManager {
	mgr := &WebhookManager{
		webhooks:      make(map[string]*WebhookConfig),
		deliveries:    make(map[string]*DeliveryEvent),
		retryQueue:    make(chan *DeliveryTask, 100),
		eventHandlers: make(map[string][]EventHandler),
		maxRetries:    3,
		baseDelay:     1 * time.Second,
		logger:        DefaultLogger{},
	}
	
	for _, opt := range opts {
		opt(mgr)
	}
	
	// Start background worker
	go mgr.processRetryQueue()
	
	return mgr
}

// WebhookManagerOption configures webhook manager
type WebhookManagerOption func(*WebhookManager)

// WithMaxRetries sets maximum retry attempts
func WithMaxRetries(max int) WebhookManagerOption {
	return func(mgr *WebhookManager) {
		mgr.maxRetries = max
	}
}

// WithBaseDelay sets initial delay between retries (exponential backoff starts here)
func WithBaseDelay(delay time.Duration) WebhookManagerOption {
	return func(mgr *WebhookManager) {
		mgr.baseDelay = delay
	}
}

// WithLogger sets custom logger
func WithLogger(logger WebhookLogger) WebhookManagerOption {
	return func(mgr *WebhookManager) {
		mgr.logger = logger
	}
}

// WithStore sets persistent store
func WithStore(store WebhookStore) WebhookManagerOption {
	return func(mgr *WebhookManager) {
		mgr.store = store
	}
}

// RegisterWebhook registers new webhook endpoint
func (mgr *WebhookManager) RegisterWebhook(config *WebhookConfig) error {
	if config.ID == "" {
		config.ID = uuid.New().String()
	}
	
	config.CreatedAt = time.Now()
	if config.Active {
		config.Active = true
	}
	
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	
	mgr.webhooks[config.ID] = config
	
	if mgr.store != nil {
		if err := mgr.store.Save(config); err != nil {
			return fmt.Errorf("save to store: %w", err)
		}
	}
	
	mgr.logger.LogWebhook(config.ID, "", "INFO", fmt.Sprintf("Registered webhook with events: %v", config.Events))
	
	return nil
}

// DeregisterWebhook removes webhook registration
func (mgr *WebhookManager) DeregisterWebhook(id string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	
	delete(mgr.webhooks, id)
	
	if mgr.store != nil {
		return mgr.store.Delete(id)
	}
	
	return nil
}

// GetWebhook retrieves webhook by ID
func (mgr *WebhookManager) GetWebhook(id string) (*WebhookConfig, error) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	
	config, exists := mgr.webhooks[id]
	if !exists {
		return nil, fmt.Errorf("webhook not found")
	}
	
	return config, nil
}

// ListWebhooks returns all registered webhooks
func (mgr *WebhookManager) ListWebhooks(activeOnly bool) ([]*WebhookConfig, error) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	
	result := make([]*WebhookConfig, 0, len(mgr.webhooks))
	
	for _, config := range mgr.webhooks {
		if !activeOnly || config.Active {
			result = append(result, config)
		}
	}
	
	return result, nil
}

// Subscribe adds event handler for specific event type
func (mgr *WebhookManager) Subscribe(eventType string, handler EventHandler) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	
	mgr.eventHandlers[eventType] = append(mgr.eventHandlers[eventType], handler)
}

// HandleEvent processes incoming event and triggers webhooks
func (mgr *WebhookManager) HandleEvent(eventType string, payload interface{}) error {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	
	// Trigger synchronous handlers first
	for _, handler := range mgr.eventHandlers[eventType] {
		if err := handler(payload); err != nil {
			return fmt.Errorf("handler error: %w", err)
		}
	}
	
	// Find matching webhooks and queue for delivery
	var matchIDs []string
	for id, config := range mgr.webhooks {
		if !config.Active {
			continue
		}
		
		// Check if webhook subscribes to this event
		for _, event := range config.Events {
			if matchesEventType(event, eventType) {
				matchIDs = append(matchIDs, id)
				
				// Check filter if present
				if config.Filters != nil && !matchesFilter(payload, config.Filters) {
					matchIDs = matchIDs[:len(matchIDs)-1]
				}
			}
		}
	}
	
	// Queue async deliveries
	for _, webhookID := range matchIDs {
		config := mgr.webhooks[webhookID]
		task := &DeliveryTask{
			Config: config,
			Event:  payload,
		}
		
		select {
		case mgr.retryQueue <- task:
			// Queued successfully
		default:
			// Queue full, drop event (implement proper backpressure in production)
		}
	}
	
	return nil
}

// matchesEventType checks if webhook event pattern matches event type
func matchesEventType(pattern, eventType string) bool {
	if pattern == eventType {
		return true
	}
	
	// Support wildcard matching
	parts := strings.Split(pattern, "*")
	if len(parts) > 1 {
		if parts[0] == "" {
			// Endswith pattern (*.event)
			return strings.HasSuffix(eventType, parts[1])
		} else if parts[1] == "" {
			// Starts with pattern (prefix.*)
			return strings.HasPrefix(eventType, parts[0])
		} else {
			// Contains pattern (prefix.*.suffix)
			return strings.Contains(eventType, parts[1])
		}
	}
	
	return false
}

// matchesFilter checks if payload matches filter criteria
func matchesFilter(payload interface{}, filters map[string]interface{}) bool {
	payloadJSON, _ := json.Marshal(payload)
	
	for _, filterValue := range filters {
		filterJSON, _ := json.Marshal(filterValue)
		
		// Simple equality check - in production implement proper JSONPath querying
		if string(payloadJSON) == string(filterJSON) {
			return true
		}
	}
	
	return false
}

// processRetryQueue handles async webhook delivery with exponential backoff
func (mgr *WebhookManager) processRetryQueue() {
	for task := range mgr.retryQueue {
		deliveryEvent := mgr.queueDelivery(task.Config, task.Event)
		
		if deliveryEvent.Status == DeliveryFailed || deliveryEvent.Status == DeliveryExhausted {
			mgr.logger.LogError(task.Config.ID, deliveryEvent.ID, deliveryEvent.LastError)
			
			// Schedule re-queue after cooldown
			if deliveryEvent.NextRetry.After(time.Now()) {
				time.AfterFunc(deliveryEvent.NextRetry.Sub(time.Now()), func() {
					mgr.retryQueue <- task
				})
			}
		}
	}
}

// queueDelivery queues a delivery event with retry logic
func (mgr *WebhookManager) queueDelivery(config *WebhookConfig, event interface{}) *DeliveryEvent {
	deliveryID := uuid.New().String()
	eventData, _ := json.Marshal(event)
	
	delivery := &DeliveryEvent{
		ID:         deliveryID,
		WebhookID:  config.ID,
		Event:      event,
		Status:     DeliveryPending,
		RetryCount: 0,
	}
	
	mgr.mu.Lock()
	mgr.deliveries[deliveryID] = delivery
	mgr.mu.Unlock()
	
	// Deliver with retry loop
	for retry := 0; retry <= mgr.maxRetries; retry++ {
		if retry > 0 {
			delay := mgr.baseDelay * (1 << (uint(retry) - 1)) // Exponential backoff
			time.Sleep(delay)
		}
		
		success, statusCode, body, err := mgr.sendRequest(config, eventData)
		
		attempt := DeliveryAttempt{
			TriedAt:     time.Now(),
			Success:     success,
			StatusCode:  statusCode,
			DurationMs:  0, // Calculate actual duration
		}
		
		if !success {
			attempt.ErrorMessage = err.Error()
			delivery.LastError = err.Error()
			delivery.Attempts = append(delivery.Attempts, attempt)
			delivery.TriedAt = append(delivery.TriedAt, time.Now())
			delivery.RetryCount++
			
			if statusCode >= 400 && statusCode < 500 {
				// Client errors don't benefit from retry
				break
			}
		} else {
			delivery.Status = DeliverySuccess
			delivery.StatusCode = statusCode
			delivery.Body = body
			delivery.Attempts = append(delivery.Attempts, attempt)
			
			if mgr.store != nil {
				mgr.store.SaveDelivery(delivery)
			}
			
			mgr.logger.LogDelivery(config.ID, deliveryID, "success")
			return delivery
		}
	}
	
	// Max retries exhausted
	delivery.Status = DeliveryExhausted
	delivery.NextRetry = time.Now().Add(1 * time.Hour) // Hourly retry thereafter
	
	return delivery
}

// sendRequest sends the webhook request with SSRF protection
func (mgr *WebhookManager) sendRequest(config *WebhookConfig, event []byte) (bool, int, string, error) {
	// Validate destination URL to prevent SSRF
	if err := mgr.validateDestinationURL(config.URL); err != nil {
		return false, 0, "", fmt.Errorf("invalid destination: %w", err)
	}
	
	req, err := http.NewRequest("POST", config.URL, bytes.NewReader(event))
	if err != nil {
		return false, 0, "", err
	}
	
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", computeHMACSHA256(event, config.Secret))
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	
	var statusCode int
	var body string
	
	if err != nil {
		return false, 0, "", err
	}
	defer resp.Body.Close()
	
	statusCode = resp.StatusCode
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	body = buf.String()
	
	return statusCode >= 200 && statusCode < 300, statusCode, body, nil
}

// computeHMACSHA256 computes HMAC-SHA256 signature
func computeHMACSHA256(data []byte, secret string) string {
	if secret == "" {
		return ""
	}
	
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return fmt.Sprintf("sha256=%x", h.Sum(nil))
}

// verifySignature verifies webhook signature from request header
func VerifySignature(payload []byte, signatureHeader string, secret string) bool {
	if secret == "" {
		return true // No validation configured
	}
	
	expectedSig := computeHMACSHA256(payload, secret)
	actualSig := extractSignature(signatureHeader)
	
	return hmac.Equal([]byte(expectedSig), []byte(actualSig))
}

// validateDestinationURL validates webhook destination URL to prevent SSRF
func (mgr *WebhookManager) validateDestinationURL(urlStr string) error {
	urlStr = strings.TrimSpace(urlStr)
	
	if !strings.HasPrefix(strings.ToLower(urlStr), "http://") && 
	   !strings.HasPrefix(strings.ToLower(urlStr), "https://") {
		return fmt.Errorf("invalid scheme: only http and https allowed")
	}
	
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	
	host := parsedURL.Hostname()
	if host == "" {
		return fmt.Errorf("empty hostname")
	}
	
	// Resolve IP addresses
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return fmt.Errorf("dns lookup: %w", err)
	}
	
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return fmt.Errorf("SSRF prevention: cannot connect to private/internal IP: %s", ip.IP)
		}
	}
	
	return nil
}

// isPrivateIP checks if IP address is private/internal
func isPrivateIP(ip net.IP) bool {
	privateCIDRs := []string{
		// Private IPv4 ranges
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		// Link-local
		"169.254.0.0/16",
		// Loopback
		"127.0.0.0/8",
		// IPv6 loopback
		"::1/128",
		// IPv6 unique local addresses (ULA)
		"fc00::/7",
		// Cloud metadata endpoints (CRITICAL!)
		// AWS EC2 Instance Metadata Service
		"169.254.169.254/32",
		// Azure IMDS
		"169.254.169.254/32",
		// GCP metadata
		"169.254.169.254/32",
	}

	for _, c := range privateCIDRs {
		_, cidr, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

func extractSignature(header string) string {
	if header == "" {
		return ""
	}

	return strings.TrimSpace(header)
}

// ReplayWebhook re-delivers failed webhook for testing/recovery
func (mgr *WebhookManager) ReplayWebhook(deliveryID string) error {
	mgr.mu.RLock()
	delivery, exists := mgr.deliveries[deliveryID]
	mgr.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("delivery not found")
	}
	
	if delivery.Status == DeliverySuccess {
		return fmt.Errorf("delivery already successful")
	}
	
	// Re-queue for retry
	config, err := mgr.GetWebhook(delivery.WebhookID)
	if err != nil {
		return err
	}
	
	mgr.retryQueue <- &DeliveryTask{
		Config: config,
		Event:  delivery.Event,
	}
	
	return nil
}

// GetDeliveryStatus returns delivery event status
func (mgr *WebhookManager) GetDeliveryStatus(deliveryID string) (*DeliveryEvent, error) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	
	delivery, exists := mgr.deliveries[deliveryID]
	if !exists {
		return nil, fmt.Errorf("delivery not found")
	}
	
	return delivery, nil
}

// GetMetrics returns webhook metrics
func (mgr *WebhookManager) GetMetrics() map[string]interface{} {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	
	total := len(mgr.webhooks)
	active := 0
	successRate := 0.0
	totalDeliveries := len(mgr.deliveries)
	successfulDeliveries := 0
	
	for _, config := range mgr.webhooks {
		if config.Active {
			active++
		}
	}
	
	for _, delivery := range mgr.deliveries {
		if delivery.Status == DeliverySuccess {
			successfulDeliveries++
		}
	}
	
	if totalDeliveries > 0 {
		successRate = float64(successfulDeliveries) / float64(totalDeliveries) * 100
	}
	
	return map[string]interface{}{
		"total_webhooks":       total,
		"active_webhooks":      active,
		"total_deliveries":     totalDeliveries,
		"successful_deliveries": successfulDeliveries,
		"success_rate_percent": successRate,
		"max_retries":          mgr.maxRetries,
		"base_delay_seconds":   mgr.baseDelay.Seconds(),
	}
}

// API Handlers for webhooks
type WebhookAPI struct {
	manager *WebhookManager
}

func NewWebhookAPI(manager *WebhookManager) *WebhookAPI {
	return &WebhookAPI{manager: manager}
}

// HandleRegister registers new webhook via API
func (api *WebhookAPI) HandleRegister(c *gin.Context) {
	var config WebhookConfig
	
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	if err := api.manager.RegisterWebhook(&config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusCreated, config)
}

// HandleList lists all webhooks
func (api *WebhookAPI) HandleList(c *gin.Context) {
	activeOnly := c.Query("active") == "true"
	
	webhooks, err := api.manager.ListWebhooks(activeOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, webhooks)
}

// HandleDelete deregisters webhook
func (api *WebhookAPI) HandleDelete(c *gin.Context) {
	id := c.Param("id")
	
	if err := api.manager.DeregisterWebhook(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Webhook deregistered"})
}

// HandleReplay replays failed delivery
func (api *WebhookAPI) HandleReplay(c *gin.Context) {
	deliveryID := c.Param("deliveryId")
	
	if err := api.manager.ReplayWebhook(deliveryID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Delivery queued for replay"})
}

// HandleGetMetrics returns webhook metrics
func (api *WebhookAPI) HandleGetMetrics(c *gin.Context) {
	metrics := api.manager.GetMetrics()
	c.JSON(http.StatusOK, metrics)
}
