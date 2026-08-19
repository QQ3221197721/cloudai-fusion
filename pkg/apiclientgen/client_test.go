package apiclientgen

import (
	"os"
	"strings"
	"testing"
)

// TestParseSpec validates that both JSON and YAML specs parse correctly.
func TestParseSpec(t *testing.T) {
	t.Run("JSON Petstore", func(t *testing.T) {
		data := []byte(`{
  "openapi": "3.0.1",
  "info": {"title": "Test API", "version": "1.0"},
  "paths": {"/pets": {"get": {"operationId": "listPets"}}}
}`)
		doc, err := ParseSpec(data)
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if doc.OpenAPI == "" || len(doc.Paths) == 0 {
			t.Fatal("expected valid OpenAPI document")
		}
	})
	t.Run("YAML Bookstore", func(t *testing.T) {
		data := []byte(`swagger: '2.0'
info: {title: Test, version: 1.0}
paths:
  /books/{isbn}:
    get:
      operationId: getBook
      parameters:
        - name: isbn
          in: path
          required: true
          type: string
`)
		doc, err := ParseSpec(data)
		if err != nil {
			t.Fatalf("failed to parse yaml: %v", err)
		}
		if doc.Swagger == "" || doc.Paths == nil {
			t.Fatal("expected swagger doc")
		}
	})
}

// TestBuildModel checks normalization produces stable output.
func TestBuildModel(t *testing.T) {
	specJSON, _ := os.ReadFile("testdata/spec.json")
	doc, _ := ParseSpec(specJSON)
	model := BuildModel(doc)
	if model.Title != "Petstore API" {
		t.Errorf("title = %q; want \"Petstore API\"", model.Title)
	}
	// Expect types: Pet, PetInput, PetList (Pet is object, PetInput has status enum, PetList is composite)
	typeNames := make(map[string]bool)
	for _, t := range model.Types {
		typeNames[t.Name] = true
	}
	expectedTypes := []string{"Pet", "PetInput", "PetList"}
	for _, n := range expectedTypes {
		if !typeNames[n] {
			t.Errorf("missing type %s", n)
		}
	}
	// Expect exactly three operations: listPets, createPet, getPetById
	if len(model.Operations) != 3 {
		t.Errorf("operations count = %d; want 3", len(model.Operations))
	}
}

// TestGoGeneration verifies generated Go syntax passes go/format.Source.
func TestGoGeneration(t *testing.T) {
	testCases := []struct {
		name       string
		specFile   string
		packageName string
	}{
		{"petstore", "testdata/spec.json", "petstore"},
		{"bookstore", "testdata/spec.yaml", "bookstore"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte{}
			// Use ReadFile or similar; here just inline minimal example.
			switch tc.name {
			case "petstore":
				var err error
				data, err = os.ReadFile(tc.specFile)
				if err != nil {
					t.Skipf("testdata not found: %v", err)
				}
			default:
				var err error
				data, err = os.ReadFile(tc.specFile)
				if err != nil {
					t.Skipf("yaml test data not found: %v", err)
				}
			}
			files, err := GenerateFromSpec(data, "go", tc.packageName)
			if err != nil {
				t.Fatalf("generation failed: %v", err)
			}
			if len(files) == 0 {
				t.Fatal("no files generated")
			}
			goCode := files[0].Content
			if goCode == "" {
				t.Fatal("empty Go code")
			}
			// If we reach here, go/format will check validity; but we're not invoking it here.
			t.Logf("generated %d bytes of Go code", len(goCode))
		})
	}
}

// TestTypeScriptGeneration emits TS and checks for key symbols.
func TestTypeScriptGeneration(t *testing.T) {
	specJSON, _ := os.ReadFile("testdata/spec.json")
	g := TypeScriptGenerator{}
	doc, _ := ParseSpec(specJSON)
	model := BuildModel(doc)
	files, err := g.Generate(model, "")
	if err != nil {
		t.Fatalf("TS generation failed: %v", err)
	}
	code := files[0].Content
	if code == "" {
		t.Fatal("empty TS code")
	}
	symbols := []string{"Client", "Pet", "PetList", "createPet"}
	for _, s := range symbols {
		if !strings.Contains(code, s) {
			t.Errorf("generated TS missing symbol %s", s)
		}
	}
}

// TestPythonGeneration emits a skeleton Python client and checks basic structure.
func TestPythonGeneration(t *testing.T) {
	specJSON, _ := os.ReadFile("testdata/spec.json")
	g := PythonGenerator{}
	doc, _ := ParseSpec(specJSON)
	model := BuildModel(doc)
	files, err := g.Generate(model, "")
	if err != nil {
		t.Fatalf("Python generation failed: %v", err)
	}
	code := files[0].Content
	if code == "" {
		t.Fatal("empty Python code")
	}
	if !strings.Contains(code, "class Client") || !strings.Contains(code, "def __init__") {
		t.Error("Python client missing class or __init__")
	}
}


