# Defensive Programming Cheat Sheet

Quick reference for defensive programming patterns in CloudAI Fusion.

---

## 🛡️ Nil Safety

### Before Accessing Fields
```go
// ❌ BAD
if user != nil {
    email := user.Email
}

// ✅ GOOD
if err := RequireNonNil(user, "user"); err != nil {
    return err
}
email := user.Email  // Safe to access
```

### Optional Fields
```go
// ✅ Use SafeDeref
discount := SafeDeref(order.DiscountRate)
if discount == 0.05 {
    fmt.Println("Default discount applied")
}
```

### Typed Nils (Slices, Maps, Pointers)
```go
// ❌ Empty slice passes nil check but isn't nil!
var items []Item          // nil slice
emptyItems := make([]Item, 0)  // empty but not nil slice

// ✅ RequireNonNil catches both
RequireNonNil(items, "items")           // Error: is nil
RequireNonNil(emptyItems, "items")      // OK: is valid empty slice
```

---

## ✅ Input Validation

### Numeric Ranges
```go
ValidateRange(value, min, max float64, fieldName string) error

// Examples
ValidateRange(float64(age), 0, 150, "age")              // age must be 0-150
ValidateRange(costPerHour, 0.0, 999.99, "cost")         // cost positive
ValidateRange(utilization, 0.0, 100.0, "utilization")   // percentage range
```

### Integer Bounds
```go
ValidateIntRange(index, min, max int, fieldName string) error

// Examples
ValidateIntRange(pageSize, 1, 100, "page_size")         // pagination
ValidateIntRange(retryCount, 0, 10, "retries")          // bounded retries
```

### Collection Access
```go
ValidateSliceBounds(index, length int, fieldName string) error

// Safe indexing
if err := ValidateSliceBounds(itemIndex, len(items), "index"); err != nil {
    return err
}
item := items[itemIndex]  // Guaranteed safe
```

### String Fields
```go
ValidateNonEmptyString(s, fieldName string) error

// Examples
ValidateNonEmptyString(userID, "user_id")        // reject empty IDs
ValidateNonEmptyString(email, "email")           // require emails
```

---

## 🔧 Error Handling

### Creating Errors
```go
// Validation errors
ValidationError(field, message) *AppError

// Not found errors
NotFound(resource, identifier) *AppError

// Wrap existing errors with context
Wrap(err, code, message) *AppError

// Extract AppErrors from chains
UnwrapAppError(err) (*AppError, bool)
```

### Common Error Codes
```go
ErrorCodeValidation       // 400 Bad Request
ErrorCodeNotFound         // 404 Not Found
ErrorCodeForbidden        // 403 Forbidden
ErrorCodeUnauthorized     // 401 Unauthorized
ErrorCodeConflict         // 409 Conflict
ErrorCodeInternal         // 500 Internal Error
ErrorCodeTimeout          // 504 Gateway Timeout
ErrorCodeRateLimitExceed  // 429 Too Many Requests
```

### Standard Handler Pattern
```go
func MyHandler(c *gin.Context) {
    result, err := DoWork(c.Request.Context())
    if err != nil {
        // Convert error chain to AppError
        appErr, ok := UnwrapAppError(err)
        if !ok {
            appErr = Wrap(err, ErrorCodeInternal, "operation failed")
        }
        
        // Send uniform response
        StandardErrorHandler(c, []error{appErr})
        c.Abort()
        return
    }
    
    c.JSON(http.StatusOK, result)
}
```

---

## 🚀 Advanced Patterns

### Guard Clause Chain
```go
func ProcessRequest(req *Request) error {
    // Early exit on invalid input
    if err := ValidateNonEmptyString(req.UserID, "user_id"); err != nil {
        return err
    }
    
    if err := RequireNonNil(req.Items, "items"); err != nil {
        return ValidationError("items", "required")
    }
    
    // Range validation
    if err := ValidateRange(float64(len(req.Items)), 1, 1000, "item_count"); err != nil {
        return err
    }
    
    // Proceed with business logic
    return executeProcessing(req)
}
```

### Try-Fallback Pattern
```go
// Safely extract value or use fallback
configPath := Try(func() (string, error) {
    path := getEnvironmentVariable("CONFIG_PATH")
    if path == "" {
        return "", fmt.Errorf("CONFIG_PATH not set")
    }
    return path, nil
}, "/etc/default/config.yaml")  // Fallback path
```

### Coalesce Multiple Defaults
```go
// Return first non-zero/non-empty value
finalValue := Coalesce([]float64{
    explicitValue,
    defaultValue,
    fallbackValue,
    0.0,  // Absolute fallback
}, 1.0)  // Default default if all are zero
```

### Safe Type Assertion
```go
// Instead of:
node, ok := obj.(*Node)
if !ok || node == nil {
    return fmt.Errorf("invalid node")
}

// Use:
node := Try(func() (*Node, error) {
    n, ok := obj.(*Node)
    if !ok || n == nil {
        return nil, fmt.Errorf("invalid node type")
    }
    return n, nil
}, &Node{})

// Now safely use node
processNode(node)
```

---

## 📊 HTTP Middleware

### Adding to Router
```go
router := gin.Default()

// Global middleware
router.Use(DefensiveMiddleware())

// Group-specific middleware
apiV1 := router.Group("/api/v1")
apiV1.Use(DefensiveMiddleware())
{
    apiV1.GET("/users/:id", handle GetUser)
    // ... routes
}
```

### Request Validator Usage
```go
validator := &RequestValidator{c: c}

// Validate URL parameters
if err := validator.ValidateParam("id"); err != nil {
    return err
}

// Validate query parameters
if err := validator.ValidateQuery("limit", required=false); err != nil {
    return err
}

// Validate request body
if err := validator.ValidateBody(); err != nil {
    return err
}
```

---

## 🐛 Debugging Tips

### Enable Stack Traces
```go
// Set environment variable
export DEBUG_DEFENSIVE=true

// Now errors include stack trace in metadata
```

### Profile Validation Overhead
```bash
go test -test.cpuprofile=profile.out ./pkg/common/defensive
go tool pprof profile.out
(pprof) top
```

### Log All Validations
```go
// Add logging wrapper
func validateWithLogging(fn func() error, fieldName string) error {
    start := time.Now()
    err := fn()
    duration := time.Since(start)
    
    if err != nil {
        logrus.WithFields(logrus.Fields{
            "field":     fieldName,
            "duration":  duration.Microseconds(),
            "error":     err.Error(),
        }).Warn("Validation failed")
    }
    
    return err
}
```

---

## 🎯 Common Scenarios

### Scenario 1: API Parameter Validation
```go
func CreateUser(c *gin.Context) {
    var req CreateUserRequest
    
    // Bind JSON
    if err := c.ShouldBindJSON(&req); err != nil {
        StandardErrorHandler(c, []error{
            ValidationError("body", "invalid JSON"),
        })
        return
    }
    
    // Validate fields
    validations := []func() error{
        func() error { return ValidateNonEmptyString(req.Name, "name") },
        func() error { return ValidateEmail(req.Email, "email") },
        func() error { return ValidateRange(float64(req.Age), 18, 120, "age") },
    }
    
    for _, validate := range validations {
        if err := validate(); err != nil {
            StandardErrorHandler(c, []error{err})
            return
        }
    }
    
    // Create user...
}
```

### Scenario 2: Database Query Safeguards
```go
func GetUserByID(ctx context.Context, id string) (*User, error) {
    // Validate ID format
    if err := ValidateNonEmptyString(id, "id"); err != nil {
        return nil, err
    }
    
    // Execute query
    user, err := database.GetUser(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, NotFound("user", id)
        }
        return nil, Wrap(err, ErrorCodeInternal, "database query failed")
    }
    
    // Verify user exists (double-check against typed nils)
    if err := RequireNonNil(user, "user"); err != nil {
        return nil, NotFound("user", id)
    }
    
    return user, nil
}
```

### Scenario 3: Configuration Loading
```go
func LoadConfig(configPath string) (*Config, error) {
    // Required config file
    if err := ValidateNonEmptyString(configPath, "config_path"); err != nil {
        return nil, err
    }
    
    // Read config with fallbacks
    configFile := Try(func() (*os.File, error) {
        return os.Open(configPath)
    }, nil)
    
    if configFile == nil {
        return nil, NotFound("config_file", configPath)
    }
    defer configFile.Close()
    
    // Parse with defaults
    config := &Config{}
    if err := json.NewDecoder(configFile).Decode(config); err != nil {
        return nil, Wrap(err, ErrorCodeInternal, "config parse failed")
    }
    
    // Apply sensible defaults
    config.Retries = Coalesce([]int{config.Retries, 3, 1}, 3)
    config.Timeout = Coalesce([]time.Duration{
        config.Timeout, 
        time.Minute, 
        30 * time.Second,
    }, time.Minute)
    
    return config, nil
}
```

---

## ⚡ Performance Quick Facts

| Check | Allocation | Time | Hot Path? |
|-------|------------|------|-----------|
| RequireNonNil | 0 | ~12ns | ✅ Yes |
| ValidateRange | 0 | ~8ns | ✅ Yes |
| SafeDeref | 0 | ~5ns | ✅ Yes |
| Wrap/AppError | 1 | ~150ns | ⚠️ Selective |
| StandardErrorHandler | 2 | ~500ns | ❌ Only on errors |

**Rule of Thumb**: Core guards (<20ns) are safe for hot paths. AppError wrapping (~150ns) acceptable for error handling code.

---

## 📚 When to Use What

| Situation | Best Practice |
|-----------|---------------|
| Checking struct pointer | `RequireNonNil(obj, "obj_name")` |
| Checking optional field | `SafeDeref(optionalField)` |
| Numeric validation | `ValidateRange(value, min, max, "field")` |
| Collection bounds | `ValidateSliceBounds(idx, len(slice), "idx")` |
| String requirements | `ValidateNonEmptyString(str, "str_field")` |
| Error wrapping | `Wrap(err, Code, "message")` |
| Not found errors | `NotFound("resource", "id")` |
| Validation failures | `ValidationError("field", "reason")` |
| HTTP handlers | Use `StandardErrorHandler(c, []error{err})` |
| Initialization only | Use `Must(err, "msg")` |

---

**Quick Tip**: Always ask "What's the worst thing that could go wrong here?" and add a guard clause to prevent it! 🛡️
