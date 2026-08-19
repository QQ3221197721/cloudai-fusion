package qa

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"gopkg.in/yaml.v3"
)

// lint.go is the Lint Rule Engine: a static analyzer for Go source code that enforces
// YAML-defined rules on imports and function-level allocation-free guarantees. It
// performs a go/ast pass over files and reports violations deterministically.
// No external linter required; it's a pure-Go gate focused on quality policies.

// LintViolation describes one found policy violation.
type LintViolation struct {
	File     string   // absolute path
	Rule     string   // e.g., "forbidden-import", "require-no-alloc-convention"
	Symbol   string   // import path or function name
	Message  string   // human-readable explanation
	Severity Severity // FAIL or WARN
}

func (v LintViolation) String() string {
	return fmt.Sprintf("%s:%s (%s): %s [%s]", v.File, v.Rule, v.Symbol, v.Message, v.Severity)
}

// Severity levels are FAIL (gate-rejecting) or WARN (informational).
type Severity int

const (
	Warn Severity = iota
	Fail
)

func (s Severity) String() string {
	if s == Fail { return "FAIL" } else { return "WARN" }
}

// LintConfig holds the loaded YAML configuration with policy rules.
type LintConfig struct {
	ForbiddenImports []string `yaml:"forbidden_imports"` // paths not allowed
	RequireNoAlloc   []string `yaml:"require_no_alloc"` // func names that must use safe patterns
}

// LintResult aggregates all violations and exposes simple pass/fail semantics
// when FailuresOnly is true.
type LintResult struct {
	Pass    bool
	Violations []LintViolation
}

// ParseYAML loads a LintConfig from disk at path.
func ParseYAL(path string) (*LintConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("qa: reading yaml: %w", err)
	}
	var cfg LintConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("qa: parsing yaml: %w", err)
	}
	return &cfg, nil
}

// LoadStringConfig creates a config from a YAML string (useful in tests).
func LoadStringConfig(yamlStr string) (*LintConfig, error) {
	var cfg LintConfig
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LintDir scans all .go files under dir for violations against cfg.
func LintDir(cfg *LintConfig, root string) (LintResult, error) {
	var result LintResult
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() { return nil }
		if !strings.HasSuffix(strings.ToLower(fi.Name()), ".go") { return nil }
		r := lintFile(cfg, fset, path)
		result.Violations = append(result.Violations, r...)
		return nil
	})
	if err != nil {
		return LintResult{}, fmt.Errorf("qa: walking dir: %w", err)
	}
	sortViolations(result.Violations)
	result.Pass = len(result.Violations) == 0
	return result, nil
}

// sortViolations sorts deterministically by File→Rule→Symbol.
func sortViolations(v []LintViolation) {
	for i := 0; i < len(v)-1; i++ {
		for j := i + 1; j < len(v); j++ {
			if less(v[i], v[j]) {
				v[i], v[j] = v[j], v[i]
			}
		}
	}
}

func less(a, b LintViolation) bool {
	if a.File != b.File { return a.File < b.File }
	if a.Rule != b.Rule { return a.Rule < b.Rule }
	return a.Symbol < b.Symbol
}

// lintFile parses a single file and returns its violations.
func lintFile(cfg *LintConfig, fset *token.FileSet, path string) []LintViolation {
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil
	}
	var vio []LintViolation
	checkForbidenImports(&vio, node, cfg.ForbiddenImports, path)
	checkNoAllocFunctions(&vio, node, cfg.RequireNoAlloc, fset, path)
	return vio
}

func checkForbidenImports(vio *[]LintViolation, n *ast.File, banned []string, path string) {
	if len(banned) == 0 { return }
	for _, imp := range n.Imports {
		raw := strings.Trim(imp.Path.Value, "\"")
		for _, p := range banned {
			if p == "*" {
				if matchesGlob(raw, "*") {
					*vio = append(*vio, LintViolation{
						File: path, Rule: "forbidden-import", Symbol: raw, Message: "matches wildcard ban", Severity: Fail,
					})
				}
			} else if strings.HasPrefix(raw, p+"/") || raw == p {
				*vio = append(*vio, LintViolation{
					File: path, Rule: "forbidden-import", Symbol: raw, Message: "explicitly forbidden", Severity: Fail,
				})
			}
		}
	}
}

func matchesGlob(s, pattern string) bool {
	if pattern == "*" { return true }
	if pattern[:2] == "**/" { return strings.Contains(s, pattern[3:]) }
	return strings.Contains(s, pattern)
}

func checkNoAllocFunctions(vio *[]LintViolation, n *ast.File, req []string, fset *token.FileSet, path string) {
	if len(req) == 0 {
		return
	}
	want := make(map[string]struct{}, len(req))
	for _, r := range req {
		want[r] = struct{}{}
	}
	for _, d := range n.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, ok := want[fn.Name.Name]; !ok {
			continue
		}
		// Real static pass: walk the body and flag AST nodes that are known
		// heap-allocation sources. This is a conservative over-approximation
		// (a function flagged here MIGHT allocate); it is deterministic and
		// requires zero runtime, unlike escape analysis.
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			kind, sym := allocKind(node)
			if kind == "" {
				return true
			}
			pos := fset.Position(node.Pos())
			*vio = append(*vio, LintViolation{
				File: path, Rule: "require-no-alloc", Symbol: fn.Name.Name,
				Message: fmt.Sprintf("line %d: %s (%s) may allocate", pos.Line, kind, sym),
				Severity: Fail,
			})
			return true
		})
	}
}

// allocKind returns a non-empty node classification when the AST node is a known
// heap-allocation source, plus a short symbol describing it. It recognizes:
//   - calls to the builtins make/new/append
//   - composite literals (slice/map/struct literals) and address-of composites
//   - string concatenation using a string literal operand
func allocKind(node ast.Node) (kind, sym string) {
	switch e := node.(type) {
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "make", "new", "append":
				return "builtin-call", id.Name
			}
		}
	case *ast.UnaryExpr:
		if e.Op == token.AND {
			if _, ok := e.X.(*ast.CompositeLit); ok {
				return "address-of-composite", "&{}"
			}
		}
	case *ast.CompositeLit:
		return "composite-literal", compositeName(e)
	case *ast.BinaryExpr:
		if e.Op == token.ADD && (isStringLit(e.X) || isStringLit(e.Y)) {
			return "string-concat", "+"
		}
	}
	return "", ""
}

func compositeName(c *ast.CompositeLit) string {
	switch t := c.Type.(type) {
	case *ast.ArrayType:
		return "slice/array"
	case *ast.MapType:
		return "map"
	case *ast.Ident:
		return t.Name
	}
	return "composite"
}

func isStringLit(x ast.Expr) bool {
	lit, ok := x.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}
