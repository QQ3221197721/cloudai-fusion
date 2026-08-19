
// Package redteam_cache - Redis caching layer for Red Team performance optimization
package redteam_cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// CacheConfig holds Redis configuration
type CacheConfig struct {
	Address         string        `json:"address"`
	Password        string        `json:"password"`
	DB              int           `json:"db"`
	DefaultTTL      time.Duration `json:"default_ttl"`
	PoolSize        int           `json:"pool_size"`
	MinIdleConns    int           `json:"min_idle_conns"`
}

// DefaultCacheConfig returns sensible defaults
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		Address:      "localhost:6379",
		Password:     "",
		DB:           0,
		DefaultTTL:   24 * time.Hour,
		PoolSize:     100,
		MinIdleConns: 10,
	}
}

// CachedClient manages Redis connection and cache operations
type CachedClient struct {
	client       *redis.Client
	config       CacheConfig
	logger       *logrus.Logger
	ctx          context.Context
	keyPrefix    string // Namespace prefix for all keys
}

// NewCachedClient creates a new Redis cache client
func NewCachedClient(cfg CacheConfig, logger *logrus.Logger) (*CachedClient, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	client := &CachedClient{
		config:    cfg,
		logger:    logger,
		ctx:       context.Background(),
		keyPrefix: "cloudai:redteam:",
	}
	
	err := client.connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	return client, nil
}

// connect establishes connection to Redis server
func (cc *CachedClient) connect() error {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cc.config.Address,
		Password:     cc.config.Password,
		DB:           cc.config.DB,
		PoolSize:     cc.config.PoolSize,
		MinIdleConns: cc.config.MinIdleConns,
	})
	
	// Test connection
	if err := rdb.Ping(cc.ctx).Err(); err != nil {
		return fmt.Errorf("Redis ping failed: %w", err)
	}
	
	cc.client = rdb
	cc.logger.WithFields(logrus.Fields{
		"address": cc.config.Address,
		"db":      cc.config.DB,
	}).Info("Connected to Redis successfully")
	
	return nil
}

// Close terminates Redis connection gracefully
func (cc *CachedClient) Close() error {
	if cc.client != nil {
		return cc.client.Close()
	}
	return nil
}

// ============================================================================
// CVE Metadata Caching (High-Frequency Read Pattern)
// ============================================================================

// CacheCVEMetadata caches CVE metadata with extended TTL
func (cc *CachedClient) CacheCVEMetadata(cveID string, metadata []byte) error {
	key := cc.makeKey("cve:metadata:" + cveID)
	
	err := cc.client.Set(cc.ctx, key, metadata, cc.config.DefaultTTL).Err()
	if err != nil {
		return fmt.Errorf("failed to cache CVE metadata: %w", err)
	}
	
	cc.logger.WithField("cve_id", cveID).Debug("CVE metadata cached")
	return nil
}

// GetCVEMetadata retrieves CVE metadata from cache
func (cc *CachedClient) GetCVEMetadata(cveID string) ([]byte, bool, error) {
	key := cc.makeKey("cve:metadata:" + cveID)
	
	data, err := cc.client.Get(cc.ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil // Cache miss
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get CVE metadata: %w", err)
	}
	
	cc.logger.WithField("cve_id", cveID).Debug("CVE metadata retrieved from cache")
	return data, true, nil
}

// DeleteCVEMetadata removes CVE metadata from cache
func (cc *CachedClient) DeleteCVEMetadata(cveID string) error {
	key := cc.makeKey("cve:metadata:" + cveID)
	
	err := cc.client.Del(cc.ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete CVE metadata: %w", err)
	}
	
	return nil
}

// ============================================================================
// Kill Chain Mapping Caching (Medium-Frequency Write Pattern)
// ============================================================================

// CacheKillChainMapping caches Kill Chain mapping results
func (cc *CachedClient) CacheKillChainMapping(cveID string, mappingJSON []byte) error {
	key := cc.makeKey("killchain:mapping:" + cveID)
	
	// Use shorter TTL for mappings since they're recomputed frequently
	err := cc.client.Set(cc.ctx, key, mappingJSON, 12*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to cache kill chain mapping: %w", err)
	}
	
	return nil
}

// GetKillChainMapping retrieves Kill Chain mapping from cache
func (cc *CachedClient) GetKillChainMapping(cveID string) ([]byte, bool, error) {
	key := cc.makeKey("killchain:mapping:" + cveID)
	
	data, err := cc.client.Get(cc.ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get kill chain mapping: %w", err)
	}
	
	return data, true, nil
}

// ============================================================================
// Attack Path Caching (Low-Frequency Write Pattern)
// ============================================================================

// CacheAttackPath caches computed attack paths
func (cc *CachedClient) CacheAttackPath(chainID string, pathJSON []byte) error {
	key := cc.makeKey("attack:path:" + chainID)
	
	// Longer TTL for attack paths (stable computations)
	err := cc.client.Set(cc.ctx, key, pathJSON, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to cache attack path: %w", err)
	}
	
	return nil
}

// GetAttackPath retrieves attack path from cache
func (cc *CachedClient) GetAttackPath(chainID string) ([]byte, bool, error) {
	key := cc.makeKey("attack:path:" + chainID)
	
	data, err := cc.client.Get(cc.ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get attack path: %w", err)
	}
	
	return data, true, nil
}

// ============================================================================
// Aggregation Results Caching (Infrequent Update Pattern)
// ============================================================================

// CacheVulnSummary caches vulnerability summary statistics
func (cc *CachedClient) CacheVulnSummary(summaryJSON []byte) error {
	key := cc.makeKey("vuln:summary")
	
	// Hourly refresh for summaries
	err := cc.client.Set(cc.ctx, key, summaryJSON, 1*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to cache vuln summary: %w", err)
	}
	
	return nil
}

// GetVulnSummary retrieves vulnerability summary from cache
func (cc *CachedClient) GetVulnSummary() ([]byte, bool, error) {
	key := cc.makeKey("vuln:summary")
	
	data, err := cc.client.Get(cc.ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get vuln summary: %w", err)
	}
	
	return data, true, nil
}

// ============================================================================
// Helper Functions
// ============================================================================

// InvalidateCVERelatedCache invalidates all CVE-related cache entries
func (cc *CachedClient) InvalidateCVERelatedCache(pattern string) error {
	keys, err := cc.client.Keys(cc.ctx, pattern).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to find keys: %w", err)
	}
	
	if len(keys) == 0 {
		return nil
	}
	
	err = cc.client.Del(cc.ctx, keys...).Err()
	if err != nil {
		return fmt.Errorf("failed to invalidate keys: %w", err)
	}
	
	cc.logger.WithField("pattern", pattern).Info("Invalidated CVE-related cache entries")
	return nil
}

// ClearAll invalidates entire cache (maintenance mode)
func (cc *CachedClient) ClearAll() error {
	err := cc.client.FlushAll(cc.ctx).Err()
	if err != nil {
		return fmt.Errorf("failed to flush cache: %w", err)
	}
	
	cc.logger.Warn("Entire cache flushed")
	return nil
}

// Stats returns Redis stats for monitoring
func (cc *CachedClient) Stats() (map[string]interface{}, error) {
	info, err := cc.client.Info(cc.ctx).Result()
	if err != nil {
		return nil, err
	}
	
	// Parse info output into map
	stats := make(map[string]interface{})
	for _, line := range strings.Split(info, "\r\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			stats[parts[0]] = parts[1]
		}
	}
	
	return stats, nil
}

// HealthCheck verifies Redis connectivity
func (cc *CachedClient) HealthCheck() error {
	return cc.client.Ping(cc.ctx).Err()
}

// makeKey constructs namespaced key
func (cc *CachedClient) makeKey(key string) string {
	return cc.keyPrefix + key
}

// MarshalSerializes data to JSON
func marshalData(data interface{}) ([]byte, error) {
	return json.Marshal(data)
}

// UnmarshalDeserializes JSON to data
func unmarshalData(jsonData []byte, v interface{}) error {
	return json.Unmarshal(jsonData, v)
}
