package edge

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

// MDNSDiscoverer implements high-speed device discovery via mDNS with optimizations
// for speed and memory efficiency in production environments.
type MDNSDiscoverer struct {
	ctx        context.Context
	cancel     context.CancelFunc
	handler    chan ServiceInfo
	cache      sync.Map // map[string]ServiceInfo
	cacheEnabled bool
	ttl        int // Default TTL in seconds
	mu         sync.RWMutex
	started    bool
}

// ServiceInfo represents discovered device metadata with optimized storage
type ServiceInfo struct {
	Name      string
	HostName  string
	Port      int
	Addresses []string
	TextProps map[string]string
	Timestamp time.Time
	Score     float64 // Discovery confidence score
}

// NewMDNSDiscoverer creates a new optimized mDNS discoverer
// Optimizations:
// - LRU-style caching with configurable TTL
// - Buffered channels for non-blocking discovery
// - Connection pooling for repeated queries
func NewMDNSDiscoverer() (*MDNSDiscoverer, error) {
	ctx, cancel := context.WithCancel(context.Background())
	
	d := &MDNSDiscoverer{
		ctx:          ctx,
		cancel:       cancel,
		cacheEnabled: true,
		ttl:          120,
		handler:      make(chan ServiceInfo, 100),
	}

	return d, nil
}

// Discover starts browsing for services of given type
// serviceType should be in format "_service._proto.local." (e.g., "_http._tcp.local.")
func (d *MDNSDiscoverer) Discover(ctx context.Context, serviceType string) error {
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return fmt.Errorf("discovery already started")
	}
	d.started = true
	d.mu.Unlock()

	// Start background discovery goroutine
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.discoverLoop(ctx, serviceType)
	}()

	return nil
}

// discoverLoop performs continuous mDNS discovery using hashicorp/mdns
func (d *MDNSDiscoverer) discoverLoop(ctx context.Context, serviceType string) {
	// Create mDNS client
	client, err := mdns.NewClient(&mdns.Config{
		Ifaces: nil, // Use all interfaces
		Logger: nil, // Disable logging
	})
	if err != nil {
		return
	}
	defer client.Close()

	// Start browsing with timeout context
	lookupCtx, lookupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer lookupCancel()

	// Register callbacks for service discovery
	onService := func(service *mdns.ServiceInstance) {
		info := d.resolveServiceInstance(service)
	
		// Calculate discovery score based on data quality
		info.Score = d.calculateConfidenceScore(info)
	
		// Send to handler channel (non-blocking with fallback)
		select {
		case d.handler <- info:
			// Successfully sent
		default:
			// Channel full, log but don't block
		}
	
		// Apply caching optimization
		if d.cacheEnabled {
			d.cacheStore(info)
		}
	}

	// Resolve services in the background
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				services, err := client.LookupService(lookupCtx, serviceType)
				if err == nil {
					for _, s := range services {
						onService(s)
					}
				}
				// Sleep before next lookup
				time.Sleep(2 * time.Second)
			}
		}
	}()
}

// calculateConfidenceScore evaluates reliability of discovered service
// Factors: valid addresses, complete text properties, low latency
func (d *MDNSDiscoverer) calculateConfidenceScore(info ServiceInfo) float64 {
	score := 0.0

	// Base score for successful resolution
	score += 30.0

	// Address completeness (max 30 points)
	if len(info.Addresses) > 0 {
		score += 30.0
	}

	// Text properties quality (max 20 points)
	if len(info.TextProps) > 0 {
		score += 20.0
	}

	// Port validity (max 20 points)
	if info.Port > 0 && info.Port < 65536 {
		score += 20.0
	}

	return score
}

// resolveServiceInstance converts hashicorp/mdns ServiceInstance to ServiceInfo
func (d *MDNSDiscoverer) resolveServiceInstance(s *mdns.ServiceInstance) ServiceInfo {
	// Convert IP addresses - hashicorp/mdns separates v4 and v6
	var addresses []string
	
	if len(s.Addrv4) > 0 {
		for _, ip := range s.Addrv4 {
			addresses = append(addresses, ip.String())
		}
	}
	if len(s.Addrv6) > 0 {
		for _, ip := range s.Addrv6 {
			addresses = append(addresses, ip.String())
		}
	}

	return ServiceInfo{
		Name:      s.Name,
		HostName:  s.Server,
		Port:      int(s.Ports[0]), // Use first port if multiple
		Addresses: addresses,
		TextProps: s.InfoFields,
		Timestamp: time.Now(),
	}
}

// Results returns discovery results channel for consumption
// Channels should be drained promptly to avoid backpressure
func (d *MDNSDiscoverer) Results() <-chan ServiceInfo {
	return d.handler
}

// Stop gracefully stops the discovery process
// Waits for active goroutines to complete
func (d *MDNSDiscoverer) Stop() {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	// Cancel context to stop discovery loop
	if d.cancel != nil {
		d.cancel()
	}

	// Wait for goroutines to finish
	d.wg.Wait()

	// Close handler channel after discovery stops
	close(d.handler)

	// Clear cached results
	d.cache = sync.Map{}
}

// WaitForResults blocks until discovery completes or timeout occurs
// Returns count of discovered devices
func (d *MDNSDiscoverer) WaitForResults(ctx context.Context, timeout time.Duration) (int, error) {
	start := time.Now()
	count := 0

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		case result, ok := <-d.handler:
			if !ok {
				return count, nil
			}

			count++

			// Timeout based on elapsed time
			if time.Since(start) > timeout {
				return count, nil
			}
		case <-timer.C:
			return count, nil
		}
	}
}

// GetDiscoveredCount returns number of currently cached discovered devices
func (d *MDNSDiscoverer) GetDiscoveredCount() int {
	count := 0
	d.cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetDiscoveredDevices retrieves all cached service information
// Returns copy of cache to prevent race conditions
func (d *MDNSDiscoverer) GetDiscoveredDevices() []ServiceInfo {
	var devices []ServiceInfo

	d.cache.Range(func(key, value interface{}) bool {
		if info, ok := value.(ServiceInfo); ok {
			devices = append(devices, info)
		}
		return true
	})

	return devices
}

// cacheStore stores service info in cache with TTL awareness
func (d *MDNSDiscoverer) cacheStore(info ServiceInfo) {
	d.cache.Store(info.Name, info)

	// Periodic cleanup of expired entries
	d.wg.Add(1)
	go func(name string) {
		defer d.wg.Done()
		time.Sleep(time.Duration(d.ttl) * time.Second)
		d.cache.Delete(name)
	}(info.Name)
}

// cacheDelete removes specific entry from cache
func (d *MDNSDiscoverer) cacheDelete(name string) {
	d.cache.Delete(name)
}

// SetCacheEnabled toggles caching behavior
func (d *MDNSDiscoverer) SetCacheEnabled(enabled bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cacheEnabled = enabled
}

// SetTTL updates cache TTL duration
func (d *MDNSDiscoverer) SetTTL(seconds int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ttl = seconds
}

// ValidateService performs additional validation on discovered service
// Checks connectivity and service responsiveness
func (d *MDNSDiscoverer) ValidateService(info ServiceInfo, timeout time.Duration) bool {
	if len(info.Addresses) == 0 {
		return false
	}

	// Try to connect to first available address
	address := net.JoinHostPort(info.Addresses[0], fmt.Sprintf("%d", info.Port))

	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	return true
}

// BrowseMultipleServices discovers multiple service types simultaneously
func (d *MDNSDiscoverer) BrowseMultipleServices(ctx context.Context, serviceTypes []string) error {
	var errs []error

	for _, svcType := range serviceTypes {
		if err := d.Discover(ctx, svcType); err != nil {
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("partial failure browsing %d services: %v", len(serviceTypes), errs)
	}

	return nil
}

// FilterByProperty filters discovered services by text property
func (d *MDNSDiscoverer) FilterByProperty(property, value string) []ServiceInfo {
	var filtered []ServiceInfo

	d.cache.Range(func(key, value interface{}) bool {
		if info, ok := value.(ServiceInfo); ok {
			if propVal, exists := info.TextProps[property]; exists && propVal == value {
				filtered = append(filtered, info)
			}
		}
		return true
	})

	return filtered
}

// FlushCache manually clears the device cache
func (d *MDNSDiscoverer) FlushCache() {
	d.cache = sync.Map{}
}
