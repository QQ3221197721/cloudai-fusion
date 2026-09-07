package quota

import (
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/license"
)

// OperationType defines the type of API operation
type OperationType string

const (
	VulnerabilityScan  OperationType = "vulnerability_scan"
	CredentialDump     OperationType = "credential_dump"
	PayloadUpload      OperationType = "payload_upload"
	LateralMovement    OperationType = "lateral_movement"
	ExploitExecution   OperationType = "exploit_execution"
	UserEnumeration    OperationType = "user_enumeration"
	SystemDiscovery    OperationType = "system_discovery"
	Persistence        OperationType = "persistence"
)

// UsageWindow represents a time window for quota tracking
type UsageWindow int

const (
	Hourly UsageWindow = iota
	Daily
	Monthly
)

// QuotaTier defines limits per license tier
type QuotaTier struct {
	Community   int // Negative = unlimited
	Professional int
	Enterprise int
}

// QuotaEnforcer tracks and enforces API usage quotas per tenant
type QuotaEnforcer struct {
	LocalCache *LocalCacheStore
	RedisClient interface{} // Optional Redis client for distributed environments
	LicenseManager *license.LicenseManager
}

// UsageRecord represents a single quota consumption event
type UsageRecord struct {
	Timestamp   time.Time
	Operation   OperationType
	TargetCount int
	TenantID    string
	RequestID   string
}

// QuotaStatus contains current quota information
type QuotaStatus struct {
	Operation    OperationType
	QuotaLimit   int
	UsedThisHour int
	UsedToday    int
	UsedThisMonth int
	Remaining    int
	IsUnlimited  bool
	ResetTime    time.Time
}

// NewQuotaEnforcer creates a new QuotaEnforcer instance
func NewQuotaEnforcer() *QuotaEnforcer {
	return &QuotaEnforcer{
		LocalCache: &LocalCacheStore{
			data: make(map[string]int),
		},
	}
}

// SetRedisClient sets up Redis client for distributed caching
func (qe *QuotaEnforcer) SetRedisClient(client interface{}) {
	qe.RedisClient = client
}

// EnforceQuota checks if tenant has remaining quota for requested operation
func (qe *QuotaEnforcer) EnforceQuota(tenantID string, operation OperationType, targetCount int, licenseInfo *license.LicenseInfo) error {
	if targetCount <= 0 {
		targetCount = 1
	}

	// Check if unlimited in license
	if isUnlimited(licenseInfo, operation) {
		return nil
	}

	// Get hourly bucket key
	hourKey := qe.getBucketKey(tenantID, operation, Hourly)
	todayKey := qe.getBucketKey(tenantID, operation, Daily)
	monthKey := qe.getBucketKey(tenantID, operation, Monthly)

	// Get usage counts
	hourlyUsage := qe.getLocalUsage(hourKey)
	dailyUsage := qe.getLocalUsage(todayKey)
	monthlyUsage := qe.getLocalUsage(monthKey)

	// Calculate quota limit based on license tier
	quotaLimit := getQuotaLimit(licenseInfo, operation)

	// Check limits
	if quotaLimit >= 0 {
		// Check monthly limit first (longest window)
		if monthlyUsage+targetCount > quotaLimit {
			return fmt.Errorf("monthly quota exceeded for %s operation (limit=%d, used=%d, requested=%d)",
				operation, quotaLimit, monthlyUsage, targetCount)
		}

		// Check daily limit
		dailyLimit := getDailyLimit(operation, quotaLimit)
		if dailyUsage+targetCount > dailyLimit {
			return fmt.Errorf("daily quota exceeded for %s operation (limit=%d, used=%d, requested=%d)",
				operation, dailyLimit, dailyUsage, targetCount)
		}

		// Check hourly limit
		hourlyLimit := getHourlyLimit(operation, quotaLimit)
		if hourlyUsage+targetCount > hourlyLimit {
			return fmt.Errorf("hourly rate limit exceeded for %s operation (limit=%d, used=%d, requested=%d)",
				operation, hourlyLimit, hourlyUsage, targetCount)
		}
	}

	// Increment counters atomically
	now := time.Now()
	qe.incrementUsage(hourKey, targetCount, now)
	qe.incrementUsage(todayKey, targetCount, now)
	qe.incrementUsage(monthKey, targetCount, now)

	// Update Redis if available
	if qe.RedisClient != nil {
		go qe.updateRedisCounters(hourKey, todayKey, monthKey, targetCount)
	}

	return nil
}

// IsWithinQuota checks quota without consuming it (read-only check)
func (qe *QuotaEnforcer) IsWithinQuota(tenantID string, operation OperationType, targetCount int, licenseInfo *license.LicenseInfo) bool {
	if targetCount <= 0 {
		targetCount = 1
	}

	if isUnlimited(licenseInfo, operation) {
		return true
	}

	hourKey := qe.getBucketKey(tenantID, operation, Hourly)
	hourlyUsage := qe.getLocalUsage(hourKey)

	quotaLimit := getQuotaLimit(licenseInfo, operation)
	hourlyLimit := getHourlyLimit(operation, quotaLimit)

	return hourlyUsage+targetCount <= hourlyLimit
}

// GetQuotaStatus retrieves current quota status for an operation
func (qe *QuotaEnforcer) GetQuotaStatus(tenantID string, operation OperationType, licenseInfo *license.LicenseInfo) *QuotaStatus {
	hourKey := qe.getBucketKey(tenantID, operation, Hourly)
	todayKey := qe.getBucketKey(tenantID, operation, Daily)
	monthKey := qe.getBucketKey(tenantID, operation, Monthly)

	status := &QuotaStatus{
		Operation:    operation,
		QuotaLimit:   getQuotaLimit(licenseInfo, operation),
		UsedThisHour: qe.getLocalUsage(hourKey),
		UsedToday:    qe.getLocalUsage(todayKey),
		UsedThisMonth: qe.getLocalUsage(monthKey),
	}

	// Calculate remaining
	if status.QuotaLimit < 0 || isUnlimited(licenseInfo, operation) {
		status.IsUnlimited = true
		status.Remaining = -1
	} else {
		status.Remaining = status.QuotaLimit - status.UsedThisMonth
	}

	// Calculate reset times
	status.ResetTime = calculateNextReset(Hourly)
	return status
}

// RecordUsage logs a quota consumption event (for auditing)
func (qe *QuotaEnforcer) RecordUsage(record UsageRecord) {
	// This could be extended to persist to database/logstash/etc.
	_ = record
}

// BulkCheckMultiple operations efficiently in one call
func (qe *QuotaEnforcer) BulkCheckMultiple(tenantID string, operations []OperationType, counts map[OperationType]int, licenseInfo *license.LicenseInfo) error {
	for _, op := range operations {
		if count := counts[op]; count > 0 {
			if err := qe.EnforceQuota(tenantID, op, count, licenseInfo); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResetQuota manually resets quota for a tenant (admin function, requires auth)
func (qe *QuotaEnforcer) ResetQuota(tenantID string, operation OperationType, windows []UsageWindow) {
	now := time.Now()
	for _, window := range windows {
		key := qe.getBucketKey(tenantID, operation, window)
		qe.clearUsage(key, now)
	}
}

// getBucketKey generates cache key for a specific time window
func (qe *QuotaEnforcer) getBucketKey(tenantID string, operation OperationType, window UsageWindow) string {
	var prefix string
	switch window {
	case Hourly:
		hour := time.Now().Unix() / 3600
		prefix = fmt.Sprintf("quota:%s:%s:h:%d", tenantID, operation, hour)
	case Daily:
		day := time.Now().Unix() / 86400
		prefix = fmt.Sprintf("quota:%s:%s:d:%d", tenantID, operation, day)
	case Monthly:
		month := time.Now().Year()*100 + int(time.Now().Month())
		prefix = fmt.Sprintf("quota:%s:%s:m:%d", tenantID, operation, month)
	}
	return prefix
}

// getLocalUsage retrieves usage from local cache
func (qe *QuotaEnforcer) getLocalUsage(key string) int {
	return qe.LocalCache.Get(key)
}

// incrementUsage increments usage counter atomically
func (qe *QuotaEnforcer) incrementUsage(key string, amount int, timestamp time.Time) {
	qe.LocalCache.Increment(key, amount)
}

// clearUsage clears usage counter
func (qe *QuotaEnforcer) clearUsage(key string, timestamp time.Time) {
	qe.LocalCache.Set(key, 0)
}

// updateRedisCounters updates Redis counters asynchronously
func (qe *QuotaEnforcer) updateRedisCounters(hourKey, todayKey, monthKey string, amount int) {
	if qe.RedisClient == nil {
		return
	}

	// Assuming Redis client has Set/Incr methods
	// In production, use actual Redis client like go-redis
	go func() {
		// Pseudo-code for Redis operations
		// qe.RedisClient.IncrBy(hourKey, amount)
		// qe.RedisClient.IncrBy(todayKey, amount)
		// qe.RedisClient.IncrBy(monthKey, amount)
	}()
}

// LocalCacheStore is a thread-safe in-memory cache
type LocalCacheStore struct {
	mutex sync.RWMutex
	data  map[string]int
	ttl   map[string]time.Time // Expiration times
}

// Get retrieves value from cache
func (lc *LocalCacheStore) Get(key string) int {
	lc.mutex.RLock()
	defer lc.mutex.RUnlock()

	if val, ok := lc.data[key]; ok {
		// Check TTL
		if expire, exists := lc.ttl[key]; exists && !expire.IsZero() {
			if time.Now().After(expire) {
				delete(lc.data, key)
				delete(lc.ttl, key)
				return 0
			}
		}
		return val
	}
	return 0
}

// Increment increases counter by amount
func (lc *LocalCacheStore) Increment(key string, amount int) {
	lc.mutex.Lock()
	defer lc.mutex.Unlock()

	currentValue := lc.data[key]
	lc.data[key] = currentValue + amount
}

// Set sets a value in cache
func (lc *LocalCacheStore) Set(key string, value int) {
	lc.mutex.Lock()
	defer lc.mutex.Unlock()

	lc.data[key] = value
}

// Clear removes a key from cache
func (lc *LocalCacheStore) Clear(key string) {
	lc.mutex.Lock()
	defer lc.mutex.Unlock()

	delete(lc.data, key)
	delete(lc.ttl, key)
}

// CleanupExpired removes expired entries
func (lc *LocalCacheStore) CleanupExpired() {
	lc.mutex.Lock()
	defer lc.mutex.Unlock()

	now := time.Now()
	for key, expire := range lc.ttl {
		if now.After(expire) {
			delete(lc.data, key)
			delete(lc.ttl, key)
		}
	}
}

// GetStats returns statistics about the cache

// isUnlimited checks if operation is unlimited for this license type
func isUnlimited(licenseInfo *license.LicenseInfo, operation OperationType) bool {
	if licenseInfo == nil {
		return false
	}

	switch licenseInfo.LicenseType {
	case license.Enterprise:
		return true
	case license.Professional:
		// Professional gets some unlimited features
		return operation == UserEnumeration
	default:
		return false
	}
}

// getQuotaLimit returns quota limit based on license level
func getQuotaLimit(licenseInfo *license.LicenseInfo, operation OperationType) int {
	if licenseInfo == nil {
		return getDefaultLimit(operation)
	}

	limit := getLimitsByTier(licenseInfo.LicenseType, operation)
	
	if limit >= 0 && licenseInfo.APIQuota > 0 {
		// Global quota applies to all operations
		globalLimit := licenseInfo.APIQuota / 10 // Divide by 10 for number of operations
		
		if globalLimit < limit {
			return globalLimit
		}
	}

	return limit
}

// getLimitsByTier returns operation-specific limits per license tier
func getLimitsByTier(licenseType license.LicenseType, operation OperationType) int {
	limits := map[OperationType]QuotaTier{
		VulnerabilityScan: {
			Community:   100,
			Professional: 1000,
			Enterprise:  -1,
		},
		CredentialDump: {
			Community:   0, // Not allowed
			Professional: 10,
			Enterprise:  -1,
		},
		PayloadUpload: {
			Community:   10,
			Professional: 100,
			Enterprise:  -1,
		},
		LateralMovement: {
			Community:   0, // Not allowed
			Professional: 5,
			Enterprise:  -1,
		},
		ExploitExecution: {
			Community:   0, // Not allowed
			Professional: 20,
			Enterprise:  -1,
		},
		UserEnumeration: {
			Community:   50,
			Professional: -1, // Unlimited for professional
			Enterprise:  -1,
		},
		SystemDiscovery: {
			Community:   20,
			Professional: 200,
			Enterprise:  -1,
		},
		Persistence: {
			Community:   0, // Not allowed
			Professional: 5,
			Enterprise:  -1,
		},
	}

	tierLimits := limits[operation]
	
	switch licenseType {
	case license.Community:
		if tierLimits.Community < 0 {
			return tierLimits.Community
		}
	case license.Professional:
		if tierLimits.Professional < 0 {
			return -1
		}
		return tierLimits.Professional
	case license.Enterprise:
		return -1 // Unlimited
	default:
		return 100 // Default
	}

	return tierLimits.Community
}

// getDefaultLimit returns default quota for unknown operations
func getDefaultLimit(operation OperationType) int {
	return 100
}

// getDailyLimit calculates daily quota from monthly
func getDailyLimit(operation OperationType, monthlyLimit int) int {
	if monthlyLimit < 0 {
		return -1
	}

	// Cap at 1/10th of monthly or 1000, whichever is smaller
	dailyCap := monthlyLimit / 10
	if dailyCap > 1000 {
		dailyCap = 1000
	}

	// Different operations have different daily patterns
	switch operation {
	case VulnerabilityScan:
		return dailyCap
	case CredentialDump, LateralMovement, ExploitExecution:
		return dailyCap / 2 // Stricter limits for dangerous ops
	default:
		return dailyCap
	}
}

// getHourlyLimit calculates hourly quota from daily
func getHourlyLimit(operation OperationType, monthlyLimit int) int {
	if monthlyLimit < 0 {
		return -1
	}

	dailyLimit := getDailyLimit(operation, monthlyLimit)
	if dailyLimit < 0 {
		return -1
	}

	// Cap at 1/24th of daily or 50, whichever is smaller
	hourlyCap := dailyLimit / 24
	if hourlyCap > 50 {
		hourlyCap = 50
	}

	// Stricter limits for dangerous operations
	switch operation {
	case CredentialDump, LateralMovement, ExploitExecution, Persistence:
		return hourlyCap / 2
	default:
		return hourlyCap
	}
}

// calculateNextReset returns when the specified window will reset
func calculateNextReset(window UsageWindow) time.Time {
	now := time.Now()
	
	switch window {
	case Hourly:
		nextHour := now.Add(time.Hour)
		return nextHour.Truncate(time.Hour)
	case Daily:
		return now.Add(24*time.Hour).Truncate(24*time.Hour)
	case Monthly:
		// Next month
		nextMonth := now.AddDate(0, 1, 0)
		return nextMonth.Truncate(24 * time.Hour)
	default:
		return now
	}
}
