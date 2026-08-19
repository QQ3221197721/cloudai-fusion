package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateAgainstRealPackage parses the docgen package itself (a real Go
// package in this repository) and verifies the emitted Markdown contains the
// key exported symbols. Note: _test.go files are excluded by ParseDir's filter,
// so we validate non-test exports only.
func TestGenerateAgainstRealPackage(t *testing.T) {
	pkg, err := ParseDir(".")
	if err != nil {
		t.Fatalf("ParseDir(.): %v", err)
	}
	if pkg.Name != "docgen" {
		t.Fatalf("package name = %q; want \"docgen\"", pkg.Name)
	}
	t.Logf("Parsed Package with %d funcs, %d vars, %d consts, %d types",
		len(pkg.Funcs), len(pkg.Vars), len(pkg.Consts), len(pkg.Types))
	for _, f := range pkg.Funcs {
		t.Logf("  Func: %s", f.Name)
	}

	dir := t.TempDir()
	g := &Generator{Dir: dir, Title: "Docgen"}
	if err := g.Generate(pkg); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	index, err := os.ReadFile(filepath.Join(dir, "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	types, err := os.ReadFile(filepath.Join(dir, "types.md"))
	if err != nil {
		t.Fatalf("read types.md: %v", err)
	}

	// index.md lists flat symbols (funcs + methods): Generate is an exported type method.
	if !strings.Contains(string(index), "Generate") {
		t.Errorf("index.md missing exported method Generate:\n%s", index)
	}
	// types.md lists exported types with their declarations and methods.
	typesStr := string(types)
	for _, sym := range []string{"Package", "Symbol", "Type", "SymbolCount"} {
		if !strings.Contains(typesStr, sym) {
			t.Errorf("types.md missing symbol %q", sym)
		}
	}
}
