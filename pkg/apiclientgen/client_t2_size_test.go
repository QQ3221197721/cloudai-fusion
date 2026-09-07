package apiclientgen

import (
	"os"
	"testing"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
	"github.com/getkin/kin-openapi/openapi3"
)

// BenchmarkOutputSize compares actual output byte sizes from both generators
func BenchmarkM40OutputSize(b *testing.B) {
	data, err := os.ReadFile(specJSONPath)
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}

	doc, _ := ParseSpec(data)
	model := BuildModel(doc)
	g := GoGenerator{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		files, err := g.Generate(model, "benchpkg")
		if err != nil || len(files) == 0 {
			b.Fatal("generation failed:", err)
		}
		_ = len(files[0].Content) // measurement only
	}
}

func BenchmarkCompetitorOAPICodeGenOutputSize(b *testing.B) {
	data, err := os.ReadFile(specJSONPath)
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}

	// Load spec (same as M40)
	loader := &openapi3.Loader{Context: nil}
	spec, err := loader.LoadFromData(data)
	if err != nil {
		b.Fatalf("Failed to load spec: %v", err)
	}

	// Create options
	cfg := codegen.Configuration{
		PackageName: "benchpkg",
		Generate:    codegen.GenerateOptions{Client: true, Models: true},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		src, err := codegen.Generate(spec, cfg)
		if err != nil {
			b.Fatalf("oapi-codegen generation failed: %v", err)
		}
		_ = len(src) // measurement only
	}
}
