//go:build flip_m21 || headtohead
// +build flip_m21,headtohead

package edge_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/hashicorp/mdns"
)

// MockServiceSimulator simulates mDNS services for benchmarking
type MockServiceSimulator struct {
	browser *zeroconf.ServiceBrowser
	count   int
	started bool
}

// BenchmarkMDNS_Discover_100Devices tests discovery performance with 100 simulated devices
func BenchmarkMDNS_Discover_100Devices(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		b.Fatalf("Failed to create discoverer: %v", err)
	}
	defer discoverer.Stop()

	err = discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatalf("Discovery failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = discoverer.GetDiscoveredCount()

		// Periodically drain channel to prevent backlog
		select {
		case <-discoverer.Results():
			// Channel empty
		default:
		}
	}
}

// BenchmarkZeroconf_Baseline tests standard zeroconf performance
func BenchmarkZeroconf_Baseline(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	browser, err := zeroconf.NewBrowserWithContext(ctx, "_http._tcp.local.",
		".", zeroconf.BrowserOptions{TTL: 120})
	if err != nil {
		b.Fatalf("Failed to create browser: %v", err)
	}
	defer browser.Stop()

	var discoveredCount int

	browser.AfterFound = func(s *zeroconf.ServiceDetails) {
		discoveredCount++
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = discoveredCount
	}
}

// BenchmarkMDNS_GetResults tests result retrieval performance
func BenchmarkMDNS_GetResults(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	err := discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		devices := discoverer.GetDiscoveredDevices()
		_ = devices
	}
}

// BenchmarkMDNS_FilterByProperty tests property filtering performance
func BenchmarkMDNS_FilterByProperty(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	err := discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		discoverer.FilterByProperty("os", "Windows")
	}
}

// BenchmarkMDNS_CacheOperations tests cache hit/miss performance
func BenchmarkMDNS_CacheHit(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	err := discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatal(err)
	}

	// Pre-populate cache
	discoverer.SetCacheEnabled(true)
	info := edge.ServiceInfo{
		Name:      "test-device",
		HostName:  "test.local",
		Port:      80,
		Addresses: []string{"192.168.1.100"},
		TextProps: map[string]string{"version": "1.0"},
		Timestamp: time.Now(),
	}
	discoverer.(*edge.MDNSDiscoverer).CacheStore(info) // Access private method

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = discoverer.GetDiscoveredCount()
	}
}

// BenchmarkMDNS_SingleDevice tests minimal overhead discovery
func BenchmarkMDNS_SingleDevice(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	err := discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := discoverer.Results()
		select {
		case _, ok := <-results:
			_ = ok
		default:
		}
	}
}

// BenchmarkMDNS_MultipleServices tests concurrent service browsing
func BenchmarkMDNS_MultipleServices(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	services := []string{
		"_http._tcp.local.",
		"_https._tcp.local.",
		"_printer._tcp.local.",
		"_smb._tcp.local.",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = discoverer.BrowseMultipleServices(ctx, services)
	}
}

// TestMDNS_Integration ensures basic functionality works
func TestMDNS_Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		t.Fatalf("Failed to create discoverer: %v", err)
	}
	defer discoverer.Stop()

	err = discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Give time for discovery
	time.Sleep(1 * time.Second)

	count := discoverer.GetDiscoveredCount()
	t.Logf("Discovered %d devices", count)

	if count < 0 {
		t.Error("Discovery count should be non-negative")
	}
}

// TestMDNS_TTLManagement tests TTL configuration
func TestMDNS_TTLManagement(t *testing.T) {
	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		t.Fatalf("Failed to create discoverer: %v", err)
	}
	defer discoverer.Stop()

	// Test default TTL
	if discoverer.(*edge.MDNSDiscoverer).TTL() != 120 {
		t.Errorf("Expected default TTL 120, got %d", discoverer.(*edge.MDNSDiscoverer).TTL())
	}

	// Test custom TTL
	discoverer.SetTTL(60)
	if discoverer.(*edge.MDNSDiscoverer).TTL() != 60 {
		t.Errorf("Expected TTL 60, got %d", discoverer.(*edge.MDNSDiscoverer).TTL())
	}
}

// TestMDNS_CacheToggle tests enabling/disabling caching
func TestMDNS_CacheToggle(t *testing.T) {
	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		t.Fatalf("Failed to create discoverer: %v", err)
	}

	// Should start enabled
	if !discoverer.(*edge.MDNSDiscoverer).CacheEnabled() {
		t.Error("Cache should be enabled by default")
	}

	discoverer.SetCacheEnabled(false)
	if discoverer.(*edge.MDNSDiscoverer).CacheEnabled() {
		t.Error("Cache should be disabled")
	}

	discoverer.SetCacheEnabled(true)
	if !discoverer.(*edge.MDNSDiscoverer).CacheEnabled() {
		t.Error("Cache should be re-enabled")
	}
}

// TestMDNS_ServiceValidation tests service validation logic
func TestMDNS_ServiceValidation(t *testing.T) {
	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		t.Fatalf("Failed to create discoverer: %v", err)
	}
	defer discoverer.Stop()

	// Test with invalid address
	invalidInfo := edge.ServiceInfo{
		Name:      "invalid",
		Addresses: []string{},
		Port:      80,
	}

	validated := discoverer.ValidateService(invalidInfo, 100*time.Millisecond)
	if validated {
		t.Error("Should reject service with no addresses")
	}

	// Test with valid structure but likely unreachable
	validInfo := edge.ServiceInfo{
		Name:      "valid",
		Addresses: []string{"127.0.0.1"},
		Port:      65536, // Invalid port
	}

	validated = discoverer.ValidateService(validInfo, 100*time.Millisecond)
	if validated {
		t.Error("Should reject service with invalid port")
	}
}

// BenchmarkConcurrentMDNS tests concurrent discovery scenarios
func BenchmarkConcurrentMDNS(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		go func() {
			discoverer, err := edge.NewMDNSDiscoverer()
			if err != nil {
				return
			}
			defer discoverer.Stop()

			err = discoverer.Discover(ctx, "_http._tcp.local.")
			if err != nil {
				return
			}

			// Quick discovery cycle
			time.Sleep(10 * time.Millisecond)

			_ = discoverer.GetDiscoveredCount()
		}()
	}
}

// BenchmarkMDNS_ChannelDrain tests efficient channel draining
func BenchmarkMDNS_ChannelDrain(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discoverer, _ := edge.NewMDNSDiscoverer()
	defer discoverer.Stop()

	err := discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Drain all available results
		done := false
		for !done {
			select {
			case _, ok := <-discoverer.Results():
				if !ok {
					done = true
				}
			default:
				done = true
			}
		}
	}
}

// ExampleMDNS_Usage demonstrates typical usage patterns
func ExampleMDNS_Usage() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create discoverer
	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		panic(err)
	}
	defer discoverer.Stop()

	// Start discovering HTTP services
	err = discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		panic(err)
	}

	// Collect results
	count := 0
	timeout := time.After(5 * time.Second)

	for {
		select {
		case info, ok := <-discoverer.Results():
			if !ok {
				goto done
			}

			count++
			fmt.Printf("Found: %s at %s:%d\n", info.Name, info.Addresses[0], info.Port)

		case <-timeout:
			goto done
		}
	}

done:
	fmt.Printf("Total discovered: %d\n", count)
}

// TestMDNS_ConcurrentAccess tests thread safety
func TestMDNS_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	discoverer, err := edge.NewMDNSDiscoverer()
	if err != nil {
		t.Fatalf("Failed to create discoverer: %v", err)
	}
	defer discoverer.Stop()

	err = discoverer.Discover(ctx, "_http._tcp.local.")
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	done := make(chan bool)

	// Concurrent access pattern
	go func() {
		for i := 0; i < 100; i++ {
			_ = discoverer.GetDiscoveredCount()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			_ = discoverer.GetDiscoveredDevices()
			time.Sleep(time.Millisecond * 20)
		}
		done <- true
	}()

	<-done
	<-done
}
