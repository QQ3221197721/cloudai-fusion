package qa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// lint_test.go exercises LintDir and YAML loading with table-driven tests.

func TestLintConfigLoad(t *testing.T) {
	yaml := "forbidden_imports: [\"unsafe\", \"github.com/vendor/*\"]\nrequire_no_alloc: [\"Parse\"]"
	cfg, err := LoadStringConfig(yaml)
	if err != nil { t.Fatalf("LoadStringConfig: %v", err) } else if len(cfg.ForbiddenImports) != 2 || cfg.RequireNoAlloc[0] != "Parse" { t.Fatalf("cfg mismatch: %v", cfg) }
}

func TestLintDirForbiddenImports(t *testing.T) {
	dir := t.TempDir()
	src := `package foo
import ("unsafe")
func F() { _ = unsafe.Sizeof(0) }`
	path := filepath.Join(dir, "foo.go")
	os.WriteFile(path, []byte(src), 0o644)
	cfg := &LintConfig{ForbiddenImports: []string{"unsafe"}}
	r, err := LintDir(cfg, dir)
	if err != nil { t.Fatalf("LintDir: %v", err) } else if r.Pass || len(r.Violations) == 0 { t.Fatalf("expected violation: %v", r) } else if !strings.Contains(r.Violations[0].Symbol, "unsafe") { t.Logf("violation symbol: %s", r.Violations[0].Symbol) }
}

func TestLintNoAllocFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `package foo
func Parse() { s := []byte{}; _ = s }` // slice literal = allocation
	path := filepath.Join(dir, "bar.go")
	os.WriteFile(path, []byte(src), 0o644)
	cfg := &LintConfig{RequireNoAlloc: []string{"Parse"}}
	r, err := LintDir(cfg, dir)
	if err != nil { t.Fatalf("LintDir: %v", err) } else if r.Pass || len(r.Violations) == 0 { t.Fatalf("expected allocation warning: %v", r) }
}
