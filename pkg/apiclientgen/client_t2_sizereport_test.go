package apiclientgen

import (
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
)

// TestT2OutputSizeReport prints the actual generated code sizes so the
// benchmark's "same work unit" claim can be honestly qualified: a generator
// that emits far more code is doing more work.
func TestT2OutputSizeReport(t *testing.T) {
	data, err := os.ReadFile(specJSONPath)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}

	// M40 output
	doc, _ := ParseSpec(data)
	model := BuildModel(doc)
	files, err := GoGenerator{}.Generate(model, "benchpkg")
	if err != nil || len(files) == 0 {
		t.Fatalf("M40 generation failed: %v", err)
	}
	m40Bytes := len(files[0].Content)
	m40Lines := countLines(files[0].Content)

	// oapi-codegen output
	loader := &openapi3.Loader{Context: nil}
	spec, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("failed to load spec: %v", err)
	}
	cfg := codegen.Configuration{
		PackageName: "benchpkg",
		Generate:    codegen.GenerateOptions{Client: true, Models: true},
	}
	src, err := codegen.Generate(spec, cfg)
	if err != nil {
		t.Fatalf("oapi-codegen generation failed: %v", err)
	}
	oapiBytes := len(src)
	oapiLines := countLines(src)

	t.Logf("M40         output: %d bytes, %d lines", m40Bytes, m40Lines)
	t.Logf("oapi-codegen output: %d bytes, %d lines", oapiBytes, oapiLines)
	t.Logf("oapi-codegen emits %.2fx more bytes, %.2fx more lines",
		float64(oapiBytes)/float64(m40Bytes),
		float64(oapiLines)/float64(m40Lines))
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
