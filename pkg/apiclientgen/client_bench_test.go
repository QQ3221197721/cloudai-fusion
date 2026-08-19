package apiclientgen

import (
	"os"
	"testing"
)

// BenchmarkParseJSON parses a medium OpenAPI spec from JSON multiple times.
func BenchmarkParseJSON(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseSpec(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseYAML parses a smaller YAML spec multiple times.
func BenchmarkParseYAML(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.yaml")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ParseSpec(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildModel validates model normalization throughput.
func BenchmarkBuildModel(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	doc, _ := ParseSpec(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildModel(doc)
	}
}

// BenchmarkGenerateGo emits Go client code repeatedly.
func BenchmarkGenerateGo(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	doc, _ := ParseSpec(data)
	model := BuildModel(doc)
	g := GoGenerator{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.Generate(model, "testpkg")
	}
}

// BenchmarkGenerateTS emits TypeScript client code repeatedly.
func BenchmarkGenerateTS(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	doc, _ := ParseSpec(data)
	model := BuildModel(doc)
	g := TypeScriptGenerator{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.Generate(model, "")
	}
}

// BenchmarkGeneratePy emits Python client code repeatedly.
func BenchmarkGeneratePy(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	doc, _ := ParseSpec(data)
	model := BuildModel(doc)
	g := PythonGenerator{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = g.Generate(model, "")
	}
}

// BenchmarkFullCycle measures end-to-end generation time in ms per operation.
func BenchmarkFullCycle(b *testing.B) {
	data, err := os.ReadFile("testdata/spec.json")
	if err != nil {
		b.Skipf("testdata not found: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files, _ := GenerateFromSpec(data, "go", "petstore")
		if len(files) == 0 {
			b.Fatal("no files generated")
		}
		_ = files[0].Content
	}
}
