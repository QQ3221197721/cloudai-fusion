package apiclientgen

import (
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

// TestT2OutputSizeComparison quantifies WHAT each tool generates so the speed
// comparison is honest: if one tool emits far more code, raw ns/op is not a
// like-for-like "same work unit". This test prints both output sizes.
func TestT2OutputSizeComparison(t *testing.T) {
	data, err := os.ReadFile(specJSONPath)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}

	// M40 output
	m40Files, err := GenerateFromSpec(data, "go", "benchpkg")
	if err != nil || len(m40Files) == 0 {
		t.Fatalf("M40 generation failed: %v", err)
	}
	m40Bytes := 0
	for _, f := range m40Files {
		m40Bytes += len(f.Content)
	}
	m40Lines := 0
	for _, f := range m40Files {
		for _, c := range f.Content {
			if c == '\n' {
				m40Lines++
			}
		}
	}

	// oapi-codegen output (client + models = closest to M40 scope)
	loader := &openapi3.Loader{Context: nil}
	spec, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("Failed to load spec: %v", err)
	}
	cfg := codegen.Configuration{
		PackageName: "benchpkg",
		Generate:    codegen.GenerateOptions{Client: true, Models: true},
	}
	oapiOut, err := codegen.Generate(spec, cfg)
	if err != nil {
		t.Fatalf("oapi-codegen generation failed: %v", err)
	}
	oapiBytes := len(oapiOut)
	oapiLines := 0
	for _, c := range oapiOut {
		if c == '\n' {
			oapiLines++
		}
	}

	t.Logf("=== OUTPUT SIZE (same OpenAPI spec, 3 endpoints) ===")
	t.Logf("M40 apiclientgen : %d bytes, %d lines, %d file(s)", m40Bytes, m40Lines, len(m40Files))
	t.Logf("oapi-codegen     : %d bytes, %d lines, 1 file", oapiBytes, oapiLines)
	t.Logf("Output ratio     : oapi-codegen emits %.1fx more bytes than M40", float64(oapiBytes)/float64(m40Bytes))
	t.Logf("")
	t.Logf("HONEST CAVEAT: oapi-codegen emits more code (typed params structs,")
	t.Logf("response wrappers, WithResponse variants, request editors). Raw ns/op")
	t.Logf("favors M40 partly because M40 emits a leaner client surface.")
}
