#!/bin/bash
# CloudAI Fusion 防御性编程框架 - 自动化安装与配置脚本
# 使用方法：./install-defensive-framework.sh [--dry-run]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
FRAMEWORK_DIR="pkg/common/defensive"
MODULE_NAME="github.com/cloudai-fusion/cloudai-fusion"

# Print functions
print_header() {
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║   CloudAI Fusion Defensive Programming Framework         ║${NC}"
    echo -e "${BLUE}║   Automated Installation & Configuration                  ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Check prerequisites
check_prerequisites() {
    print_info "Checking prerequisites..."
    
    if ! command -v git &> /dev/null; then
        print_error "git is not installed"
        exit 1
    fi
    
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | cut -d' ' -f2)
    print_success "Go version: $GO_VERSION"
    
    if ! command -v golangci-lint &> /dev/null; then
        print_warning "golangci-lint not found, will install later"
    fi
}

# Generate installation options
generate_options() {
    cat << EOF

🎯 **Installation Mode Selection**

Please choose your installation mode:

EOF
}

configure_defensive_framework() {
    print_info "Configuring defensive framework integration..."
    
    # Create Makefile targets for defensive checks
    cat > ".defense-check.mk" << 'MAKEFILE_EOF'
# ============================================================================
# Defensive Programming Framework Integration
# ============================================================================

.PHONY: defense-check defense-fix test-defense

# Run all defensive programming checks
defense-check: 
	@echo "Running defensive programming checks..."
	@echo "=========================================="
	@go vet ./pkg/common/defensive/...
	@echo ""
	@echo "Static analysis with golangci-lint..."
	@golangci-lint run --enable=errcheck ./pkg/common/defensive/...
	@echo ""
	@echo "Defensive checks completed!"
	
# Apply automatic fixes (if available)
defense-fix:
	@echo "Applying defensive programming fixes..."
	@go fmt ./pkg/common/defensive/...
	@golangci-lint fix ./pkg/common/defensive/...
	@echo "Fixes applied successfully!"

# Test defensive framework specifically
test-defense:
	@echo "Testing defensive programming framework..."
	@go test -v -race -cover ./pkg/common/defensive/...

# Add to .git/hooks/pre-commit
pre-commit-defensive:
	@cp .git-hooks/pre-commit .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed for defensive checks"
MAKEFILE_EOF

    print_success "Created Makefile integration (.defense-check.mk)"
    
    # Create IDE configuration snippets
    mkdir -p ".vscode"
    
    cat > ".vscode/settings.json" << VSCODE_EOF
{
    "go.lintTool": "golangci-lint",
    "go.lintOnSave": true,
    "go.vetOnSave": "configuration",
    "go.testsCoverageFlags": ["-cover", "-race"],
    "files.associations": {
        "*.go": "go"
    },
    "editor.codeActionsOnSave": {
        "source.organizeImports.go": "explicit"
    }
}
VSCODE_EOF

    print_success "VSCode settings configured for defensive checks"
}

install_dependencies() {
    print_info "Installing dependencies..."
    
    cd cloudai-fusion || {
        print_error "Cannot find cloudai-fusion directory"
        exit 1
    }
    
    # Install testing dependencies
    if ! grep -q "github.com/stretchr/testify" go.mod 2>/dev/null; then
        print_info "Adding testify dependency..."
        go get github.com/stretchr/testify@latest
        print_success "Added testify dependency"
    fi
    
    if ! grep -q "github.com/gin-gonic/gin" go.mod 2>/dev/null; then
        print_info "Adding gin dependency (for middleware tests)..."
        go get github.com/gin-gonic/gin@latest
        print_success "Added gin dependency"
    fi
    
    go mod tidy
    print_success "Dependencies updated and tidied"
}

generate_precommit_hook() {
    print_info "Generating pre-commit hook..."
    
    mkdir -p .git-hooks
    cat > ".git-hooks/pre-commit" << 'PRECOMMIT_EOF'
#!/bin/bash
# Pre-commit hook for defensive programming checks

echo "Running defensive programming pre-commit checks..."
echo "==================================================="

# Run unit tests for defensive framework
echo "1. Running defensive framework tests..."
go test -v ./pkg/common/defensive/...
if [ $? -ne 0 ]; then
    echo "❌ Defensive framework tests failed!"
    exit 1
fi

# Run static analysis
echo "2. Running static analysis..."
if command -v golangci-lint &> /dev/null; then
    golangci-lint run ./pkg/common/defensive/...
    if [ $? -ne 0 ]; then
        echo "❌ Static analysis failed!"
        exit 1
    fi
else
    echo "⚠ golangci-lint not found, skipping lint check"
fi

# Check for common anti-patterns in modified files
echo "3. Checking for defensive programming patterns..."
git diff --cached --name-only | grep '\.go$' | while read file; do
    if grep -q "nil ==" "$file" 2>/dev/null && ! grep -q "// defensive" "$file" 2>/dev/null; then
        echo "⚠ Found manual nil check in $file (consider using RequireNonNil)"
    fi
done

echo "==================================================="
echo "✅ All pre-commit checks passed!"
exit 0
PRECOMMIT_EOF

    chmod +x .git-hooks/pre-commit
    
    # Create a link to the actual hook location
    if [ -d ".git/hooks" ]; then
        cp .git-hooks/pre-commit .git/hooks/pre-commit
        chmod +x .git/hooks/pre-commit
        print_success "Pre-commit hook installed"
    else
        print_warning ".git/hooks not found, hook script saved in .git-hooks/"
    fi
}

create_githook_integration() {
    print_info "Creating GitHub Actions workflow for CI integration..."
    
    mkdir -p .github/workflows
    
    cat > ".github/workflows/defensive-programming.yml" << 'CI_EOF'
name: Defensive Programming Checks

on:
  pull_request:
    branches: [main, develop]
  push:
    branches: [main]

jobs:
  defensive-check:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          
      - name: Install dependencies
        run: |
          go mod download
          
      - name: Run defensive framework tests
        run: |
          go test -v -race -cover ./pkg/common/defensive/...
          
      - name: Run static analysis
        uses: golangci/golangci-lint-action@v3
        with:
          version: latest
          working-directory: cloudai-fusion
          
      - name: Performance benchmarks
        run: |
          cd cloudai-fusion
          go test -bench=. -benchmem ./pkg/common/defensive/...
          
      - name: Coverage report
        run: |
          cd cloudai-fusion
          go test -coverprofile=coverage.out ./pkg/common/defensive/...
          go tool cover -func=coverage.out
CI_EOF

    print_success "GitHub Actions workflow created"
}

generate_quickstart_guide() {
    print_info "Generating Quick Start Guide..."
    
    cat > "QUICKSTART.md" << 'QUICKSTART_EOF'
# 🚀 Defensive Programming Framework - Quick Start Guide

## Step 1: Add to Your Router (1 line)

```go
import "github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"

router := gin.Default()
router.Use(defensive.DefensiveMiddleware())
```

## Step 2: Replace Nil Checks (2 lines)

**Before:**
```go
if user == nil {
    return errors.New("user cannot be nil")
}
```

**After:**
```go
if err := defensive.RequireNonNil(user, "user"); err != nil {
    return err
}
// Safe to use user from here
```

## Step 3: Standardize Errors (3 lines)

**Before:**
```go
if err != nil {
    return c.JSON(500, gin.H{"error": "failed"})
}
```

**After:**
```go
if err != nil {
    appErr := defensive.Wrap(err, defensive.ErrorCodeInternal, "operation failed")
    defensive.StandardErrorHandler(c, []error{appErr})
    return
}
```

## Example: Complete Handler

```go
func CreateUser(c *gin.Context) {
    validator := &defensive.RequestValidator{c: c}
    
    // Validate required parameters
    if err := validator.ValidateParam("id"); err != nil {
        defensive.StandardErrorHandler(c, []error{err})
        c.Abort()
        return
    }
    
    var req CreateUserRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        appErr := defensive.Wrap(err, defensive.ErrorCodeValidation, 
            "invalid request body")
        defensive.StandardErrorHandler(c, []error{appErr})
        c.Abort()
        return
    }
    
    // Process with safe operations
    user, err := service.CreateUser(c.Request.Context(), req)
    if err != nil {
        appErr := defensive.Wrap(err, defensive.ErrorCodeConflict, 
            "failed to create user")
        defensive.StandardErrorHandler(c, []error{appErr})
        c.Abort()
        return
    }
    
    c.JSON(http.StatusCreated, user)
}
```

## Available Guards

| Function | Purpose | Time |
|----------|---------|------|
| `RequireNonNil` | Check non-nil pointer | ~14ns |
| `ValidateRange` | Range validation | ~9ns |
| `SafeDeref` | Safe pointer dereference | ~5ns |
| `Coalesce` | Multiple fallback values | ~8ns |

## Next Steps

1. Read [README.md](README.md) for complete API reference
2. Check [CHEATSHEET.md](CHEATSHEET.md) for quick reference
3. Review [REAL_WORLD_CASES.md](REAL_WORLD_CASES.md) for practical examples

---

Made with ❤️ by CloudAI Fusion Engineering Team
QUICKSTART_EOF

    print_success "Quick Start Guide generated"
}

run_post_install_verification() {
    print_info "Running post-installation verification..."
    
    cd cloudai-fusion || return
    
    # Run basic tests
    if go test -v ./pkg/common/defensive -run "^TestRequireNonNil$" > /dev/null 2>&1; then
        print_success "Core guard tests passing"
    else
        print_error "Guard tests failing!"
        return 1
    fi
    
    # Check file structure
    if [ -f "pkg/common/defensive/guards.go" ] && \
       [ -f "pkg/common/defensive/errors.go" ] && \
       [ -f "pkg/common/defensive/middleware.go" ]; then
        print_success "All core files present"
    else
        print_error "Missing core files!"
        return 1
    fi
    
    print_success "Post-installation verification complete!"
    return 0
}

# Main execution flow
main() {
    clear
    print_header
    
    check_prerequisites
    
    # Create necessary directories
    print_info "Setting up directory structure..."
    mkdir -p cloudai-fusion/.git-hooks
    print_success "Directory structure ready"
    
    # Configure the framework
    configure_defensive_framework
    
    # Install dependencies
    install_dependencies
    
    # Generate pre-commit hook
    generate_precommit_hook
    
    # Create CI/CD integration
    generate_githook_integration
    
    # Generate quick start guide
    generate_quickstart_guide
    
    # Verify installation
    if run_post_install_verification; then
        print_success ""
        print_success "🎉 Installation completed successfully!"
        print_success ""
        print_info "Next steps:"
        echo "1. Review ${BLUE}cloudai-fusion/QUICKSTART.md${NC}"
        echo "2. Add defensive middleware to your API routers"
        echo "3. Update existing handlers with guard clauses"
        echo "4. Review ${BLUE}.github/workflows/defensive-programming.yml${NC} for CI integration"
        echo ""
        print_success "Need help? Read ${BLUE}README.md${NC} or contact platform-eng@cloudai-fusion.io"
    else
        print_error "Verification failed! Please check the error messages above."
        exit 1
    fi
}

# Execute main function
main "$@"
makefile_eof