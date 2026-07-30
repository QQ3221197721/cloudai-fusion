# CloudAI Fusion WASM Sandbox Security Configuration

**Version**: 1.0  
**Status**: Production Ready  
**Last Updated**: 2026-08-05  

---

## 🎯 Overview

The WASM (WebAssembly) Sandbox provides hardware-isolated, resource-constrained execution environment for untrusted plugins and extensions with comprehensive security guarantees.

### Security Guarantees

✅ **Resource Isolation**: CPU, memory, syscall limits enforced  
✅ **Network Isolation**: Default deny network access  
✅ **File System Protection**: Read-only mounts by default  
✅ **Time Constraints**: Execution timeouts prevent DoS  
✅ **Privilege Restrictions**: No privileged operations allowed  
✅ **Audit Trail**: Complete execution logging with metrics  

---

## 🔧 Quick Start

### Basic Plugin Execution

```go
import "github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"

// Configure security settings
config := wasm.SecurityConfig{
    CPULimit:        2.0,         // Max 2 CPU cores
    MemoryLimitMB:   256,          // Max 256MB RAM
    SyscallLimit:    10000,        // Max 10k syscalls
    TimeLimitSec:    60,           // Max 60s execution
    NetworkEnabled:  false,        // Disable network (default)
    DiskAccess:      false,        // Disable disk writes (default)
}

// Create hardened executor
executor, err := wasm.NewHardenedPluginExecutor(config)
if err != nil {
    log.Fatal(err)
}
defer executor.Shutdown()

// Execute plugin
ctx := context.Background()
pluginCode := []byte(`console.log('Hello from plugin')`)
input := []byte(`{"data": "test"}`)

result, err := executor.ExecutePlugin(ctx, "my-plugin", pluginCode, input)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Success: %v, Duration: %dms\n", result.Success, result.DurationMs)
```

---

## 📋 Configuration Reference

### SecurityConfig Fields

| Field | Type | Default | Min | Max | Description |
|-------|------|---------|-----|-----|-------------|
| `CPULimit` | float64 | 2.0 | 0 | 8 | Maximum CPU cores to allocate |
| `MemoryLimitMB` | int | 256 | 0 | 4096 | Maximum memory in MB |
| `SyscallLimit` | int | 10000 | 0 | 100000 | Maximum system calls allowed |
| `TimeLimitSec` | int | 60 | 1 | 300 | Maximum execution time in seconds |
| `NetworkEnabled` | bool | false | - | - | Allow network access |
| `DiskAccess` | bool | false | - | - | Allow file system writes |
| `AllowPrivileged` | bool | false | - | - | Enable privileged mode |

### Recommended Configurations

#### Development Mode
```go
config := wasm.SecurityConfig{
    CPULimit:     4.0,
    MemoryLimitMB: 512,
    SyscallLimit: 50000,
    TimeLimitSec: 300,  // 5 minutes
    NetworkEnabled: true,   // Allow for testing
    DiskAccess:     true,   // For file output testing
}
```

#### Production Mode (Default)
```go
config := wasm.DefaultSecurityConfig()
// Same as default with strict limits and disabled features
```

#### High-Security Mode
```go
config := wasm.SecurityConfig{
    CPULimit:        1.0,
    MemoryLimitMB:   128,
    SyscallLimit:    5000,
    TimeLimitSec:    30,
    NetworkEnabled:  false,
    DiskAccess:      false,
}
```

---

## 🔒 Resource Limits Explained

### CPU Limiting
```go
// Set CPU limit to 2 cores
config.CPULimit = 2.0

// In practice: cgroups v2 cpuset.cpus = "0-1"
```

**Impact**: Plugins cannot consume more than configured CPU resources regardless of complexity.

### Memory Limiting
```go
// Set memory limit to 256MB
config.MemoryLimitMB = 256

// In practice: cgroups v2 memory.max = "268435456"
```

**Impact**: Prevents memory exhaustion attacks via plugin exploitation.

### Syscall Limiting
```go
// Limit to 10000 system calls
config.SyscallLimit = 10000

// In practice: Monitored via seccomp-bpf filtering
```

**Impact**: Prevents runaway loops or malicious syscall storms.

### Time Limiting
```go
// Timeout after 60 seconds
config.TimeLimitSec = 60

// In practice: cgroups v2 cpu.max with period
```

**Impact**: Prevents infinite loops and denial-of-service attacks.

---

## 🌐 Network Controls

### Allowed Hosts Configuration
```go
filter := wasm.NewNetworkFilter(
    []string{
        "api.cloudai-fusion.io",
        "external-api.example.com",
    },
    []int{22, 23, 25, 135, 139, 445},  // Blocked ports
)

// Only connections to allowlisted hosts on non-blocked ports permitted
allowed := filter.CanConnect("api.cloudai-fusion.io", 443)  // true
blocked := filter.CanConnect("evil.com", 80)               // false
```

### Blocking Dangerous Ports
```go
// Common blocked ports
dangerousPorts := []int{
    22,   // SSH
    23,   // Telnet
    25,   // SMTP
    135,  // Windows RPC
    139,  // NetBIOS
    445,  // SMB
}
```

---

## 💾 File System Controls

### Mount Points Configuration
```go
guard := wasm.NewFileSystemGuard(
    // Read-only mount points
    roMounts: []string{"/etc", "/usr", "/bin"},
    
    // Read-write mount points (restricted)
    rwMounts: []string{"/tmp/plugins", "/var/cache"},
    
    // Temporary directory for plugin outputs
    tempDir: "/tmp/plugin-execution",
)

// Can write?
guard.CanWrite("/tmp/plugins/output.json")  // true (in RW mount)
guard.CanWrite("/etc/passwd")              // false (RO mount)
guard.CanWrite("/home/user/data")          // false (not mounted)
```

### Write Limiting
```go
guard.WriteLimit = 100  // Max 100 writes per session

// After 100 writes, further attempts denied
guard.CanWrite("/tmp/limit-test.json")  // false after 100 writes
```

---

## 📊 Monitoring & Metrics

### Executor Health Metrics
```go
metrics := executor.GetMetrics()

// Example output:
map[string]interface{}{
    "is_initialized": true,
    "uptime_seconds": 3600,  // 1 hour
    "plugins_executed": 150,
    "cache_hits": 120,
    "cache_misses": 30,
    "hit_rate_percent": 80.0,
    "resource_monitor": map[string]interface{}{
        "cpu_usage_percent": 45.5,
        "memory_usage_mb": 128,
        "syscall_count": 8500,
        "execution_time_sec": 25.3,
    },
}
```

### Per-Execution Metrics
```go
result, _ := executor.ExecutePlugin(ctx, "plugin-name", code, input)

// Result includes detailed resource usage:
result.ResourceUsage = map[string]interface{}{
    "cpu_usage_percent": 75.2,
    "memory_usage_mb": 200,
    "syscall_count": 9500,
    "syscall_limit": 10000,
    "execution_time_sec": 45.8,
    "time_limit_sec": 60,
    "limit_exceeded": false,
}
```

---

## 🔍 Security Audit Checklist

Before deploying to production, verify:

### Core Security
- [ ] All CPU/memory/syscall/time limits properly configured
- [ ] Network access disabled by default
- [ ] Disk write access restricted to specific directories
- [ ] Privileged mode completely disabled
- [ ] Security config validated on startup

### Runtime Monitoring
- [ ] Real-time resource monitoring enabled
- [ ] Alert thresholds configured for limit violations
- [ ] Execution logs retained for audit trail
- [ ] Metrics exported to monitoring system

### Operational Hardening
- [ ] Plugin code signed and verified
- [ ] Plugin repository integrity verified
- [ ] Access controls restricting who can deploy plugins
- [ ] Regular security audits scheduled
- [ ] Incident response plan documented

---

## 🚨 Incident Response

### If Plugin Exceeds Limits

1. **Immediate Actions**:
   ```go
   if limitExceeded {
       // Cancel execution immediately
       executor.cancelFunc()
       
       // Log incident
       log.WithFields(logrus.Fields{
           "plugin_name": pluginName,
           "limit_type":  exceededLimit,
           "timestamp":   time.Now(),
       }).Error("Plugin exceeded resource limits")
   }
   ```

2. **Investigation Steps**:
   - Review execution logs for root cause
   - Check if malicious or accidental
   - Analyze resource utilization patterns
   - Determine if plugin needs updates

3. **Remediation**:
   - Block problematic plugin hash
   - Update security configuration if needed
   - Deploy patch to affected deployments

---

## 📖 Performance Guidelines

### Optimal Configuration for Typical Workloads

| Plugin Type | CPU Limit | Memory | Syscalls | Time |
|------------|-----------|--------|----------|------|
| Simple filters | 0.5 core | 64MB | 1000 | 10s |
| Data transformations | 1.0 core | 128MB | 5000 | 30s |
| ML inference | 2.0 cores | 512MB | 10000 | 60s |
| Batch processing | 4.0 cores | 1GB | 50000 | 300s |

### Benchmarks (Reference)

```
Simple Plugin (filter):
  Throughput: ~1000 executions/sec
  Latency: <5ms average
  
Data Transformation:
  Throughput: ~100 executions/sec
  Latency: <50ms average
  
ML Inference:
  Throughput: ~10 executions/sec
  Latency: <100ms average
```

---

## 🔧 Advanced Features

### Plugin Caching
```go
// Enable plugin caching for repeated executions
executor.EnableCaching(true)

// Cache automatically reuses previously executed plugin code
// Reduces startup overhead significantly
```

### Custom Tracer Integration
```go
import oteltrace "go.opentelemetry.io/otel/trace"

tracer := oteltrace.TracerProvider().Tracer("wasm-executor")
executor.SetTracer(tracer)

// Executions now trace across distributed tracing system
```

### Dynamic Configuration Updates
```go
// Update limits at runtime without restart
executor.UpdateSecurityConfig(wasm.SecurityConfig{
    MemoryLimitMB: 512,  // Increase from 256MB
})

// Changes apply to next execution only
```

---

## 📞 Support & Resources

**Documentation**: https://docs.cloudai-fusion.io/wasm-sandbox  
**Issue Tracker**: github.com/cloudai-fusion/cloudai-fusion/issues  
**Security Contacts**: security@cloudai-fusion.io  
**Community Slack**: #wasm-sandbox channel  

---

**Document Version**: 1.0  
**Author**: CloudAI Fusion Security Team  
**Review Cycle**: Quarterly  
**Next Review Date**: November 2026

🔒 **Secure Plugin Execution is Critical - Use These Guidelines!**
