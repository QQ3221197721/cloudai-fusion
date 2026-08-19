package docgen

import (
	"fmt"
	"testing"
)

// docgen_bench_test.go holds the performance-validation benchmarks for the
// Module 43 documentation generator. Every benchmark exercises the real public
// API (ParseDir / Generator.Generate / Package.AllSymbols) against genuine Go
// packages in this repository or against synthetically-built Package models, so
// the reported numbers reflect actual go/ast + go/doc parsing and text/template
// rendering costs — no mocks, no stubbed I/O.

// mediumPkgDir is a real, moderately sized in-repo package used to exercise the
// parser on a larger AST than docgen itself. It is parsed relative to the
// docgen package directory (the working directory during `go test`).
const mediumPkgDir = "../scheduler"

// synthPackage builds a Package populated with roughly n exported symbols spread
// across funcs, consts, vars and types (each type carrying a couple of fields
// and methods). It lets the generation benchmarks scale independently of any
// on-disk source, so BenchmarkGenerateDoc_* measure template rendering cost at a
// controlled symbol count.
func synthPackage(n int) *Package {
	p := &Package{
		Name:       "synth",
		ImportPath: "github.com/example/synth",
		Doc:        "Package synth is a synthetic package used for docgen generation benchmarks.",
	}
	// Distribute the symbol budget: ~40% funcs, ~15% consts, ~15% vars, ~30% types.
	nFuncs := n * 40 / 100
	nConsts := n * 15 / 100
	nVars := n * 15 / 100
	nTypes := n - nFuncs - nConsts - nVars // remainder as types
	if nTypes < 0 {
		nTypes = 0
	}

	for i := 0; i < nFuncs; i++ {
		p.Funcs = append(p.Funcs, Symbol{
			Name:      fmt.Sprintf("Func%d", i),
			Signature: fmt.Sprintf("func Func%d(ctx context.Context, id string) (Result, error)", i),
			Doc:       fmt.Sprintf("Func%d performs synthetic operation number %d and returns a Result.", i, i),
		})
	}
	for i := 0; i < nConsts; i++ {
		p.Consts = append(p.Consts, Symbol{
			Name:      fmt.Sprintf("Const%d", i),
			Signature: fmt.Sprintf("const Const%d = %d", i, i),
			Doc:       fmt.Sprintf("Const%d is synthetic constant %d.", i, i),
		})
	}
	for i := 0; i < nVars; i++ {
		p.Vars = append(p.Vars, Symbol{
			Name:      fmt.Sprintf("Var%d", i),
			Signature: fmt.Sprintf("var Var%d Result", i),
			Doc:       fmt.Sprintf("Var%d is synthetic variable %d.", i, i),
		})
	}
	for i := 0; i < nTypes; i++ {
		t := Type{
			Name: fmt.Sprintf("Type%d", i),
			Kind: "struct",
			Decl: fmt.Sprintf("type Type%d struct { ID string; Value int }", i),
			Doc:  fmt.Sprintf("Type%d is synthetic struct type %d.", i, i),
			Fields: []Field{
				{Name: "ID", Type: "string", Tag: "`json:\"id\"`", Doc: "identifier"},
				{Name: "Value", Type: "int", Tag: "`json:\"value\"`", Doc: "payload value"},
			},
			Methods: []Symbol{
				{Name: "String", Recv: fmt.Sprintf("Type%d", i), Signature: "func (t Type" + fmt.Sprint(i) + ") String() string", Doc: "String implements fmt.Stringer."},
				{Name: "Validate", Recv: fmt.Sprintf("Type%d", i), Signature: "func (t Type" + fmt.Sprint(i) + ") Validate() error", Doc: "Validate checks invariants."},
			},
		}
		p.Types = append(p.Types, t)
	}
	return p
}

// BenchmarkParseDir_Small parses the docgen package itself (a small real
// package: 2 source files) end-to-end via go/parser + go/doc.
func BenchmarkParseDir_Small(b *testing.B) {
	// Sanity check outside the timed loop.
	if _, err := ParseDir("."); err != nil {
		b.Fatalf("ParseDir(.): %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkg, err := ParseDir(".")
		if err != nil {
			b.Fatalf("ParseDir(.): %v", err)
		}
		if pkg.Name != "docgen" {
			b.Fatalf("unexpected package name %q", pkg.Name)
		}
	}
}

// BenchmarkParseDir_Medium parses a larger real in-repo package (pkg/scheduler,
// ~20 source files) to measure parser cost at a realistic module size.
func BenchmarkParseDir_Medium(b *testing.B) {
	pkg, err := ParseDir(mediumPkgDir)
	if err != nil {
		b.Fatalf("ParseDir(%s): %v", mediumPkgDir, err)
	}
	b.Logf("medium package %q parsed with %d symbols", pkg.Name, pkg.SymbolCount())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseDir(mediumPkgDir); err != nil {
			b.Fatalf("ParseDir(%s): %v", mediumPkgDir, err)
		}
	}
}

// BenchmarkGenerateDoc_Small renders Markdown for a package of ~100 symbols.
func BenchmarkGenerateDoc_Small(b *testing.B) {
	pkg := synthPackage(100)
	b.Logf("synthetic small package has %d symbols", pkg.SymbolCount())
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := &Generator{Dir: dir, Title: "Synth Small"}
		if err := g.Generate(pkg); err != nil {
			b.Fatalf("Generate: %v", err)
		}
	}
}

// BenchmarkGenerateDoc_Large renders Markdown for a package of ~1200 symbols,
// stressing text/template rendering and the AllSymbols flattening path.
func BenchmarkGenerateDoc_Large(b *testing.B) {
	pkg := synthPackage(1200)
	b.Logf("synthetic large package has %d symbols", pkg.SymbolCount())
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := &Generator{Dir: dir, Title: "Synth Large"}
		if err := g.Generate(pkg); err != nil {
			b.Fatalf("Generate: %v", err)
		}
	}
}

// BenchmarkFullCycle exercises the complete pipeline: ParseDir → Generator.Generate
// (which renders index.md + types.md and serializes them to disk).
func BenchmarkFullCycle(b *testing.B) {
	if _, err := ParseDir("."); err != nil {
		b.Fatalf("ParseDir(.): %v", err)
	}
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pkg, err := ParseDir(".")
		if err != nil {
			b.Fatalf("ParseDir(.): %v", err)
		}
		g := &Generator{Dir: dir, Title: "Docgen Full Cycle"}
		if err := g.Generate(pkg); err != nil {
			b.Fatalf("Generate: %v", err)
		}
	}
}
