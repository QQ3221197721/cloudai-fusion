// Package apiclientgen generator orchestration: the Generator interface, the
// registry of built-in generators, and shared identifier-casing helpers.
package apiclientgen

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// GeneratedFile is a single emitted source file.
type GeneratedFile struct {
	Path    string // suggested relative file name, e.g. "client.go"
	Content string
}

// Generator emits client source for one target language from a Model.
type Generator interface {
	// Language is the canonical target language name (e.g. "go").
	Language() string
	// Generate produces one or more source files for the given package name.
	Generate(m *Model, packageName string) ([]GeneratedFile, error)
}

// registry holds the built-in generators keyed by language.
var registry = map[string]Generator{
	"go":         &GoGenerator{},
	"typescript": &TypeScriptGenerator{},
	"python":     &PythonGenerator{},
}

// Languages returns the sorted list of supported target languages.
func Languages() []string {
	out := make([]string, 0, len(registry))
	for l := range registry {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// GeneratorFor returns the generator registered for lang, or an error.
func GeneratorFor(lang string) (Generator, error) {
	g, ok := registry[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		return nil, fmt.Errorf("apiclientgen: unsupported language %q (supported: %s)", lang, strings.Join(Languages(), ", "))
	}
	return g, nil
}

// GenerateFromSpec is the top-level convenience entry point: parse a raw spec,
// normalize it, and emit client files for the requested language.
func GenerateFromSpec(spec []byte, lang, packageName string) ([]GeneratedFile, error) {
	doc, err := ParseSpec(spec)
	if err != nil {
		return nil, err
	}
	gen, err := GeneratorFor(lang)
	if err != nil {
		return nil, err
	}
	return gen.Generate(BuildModel(doc), packageName)
}

// --- shared identifier helpers ---

// splitWords tokenizes an identifier into words, splitting on non-alphanumeric
// boundaries and camelCase transitions.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	prevLower := false
	for _, r := range s {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.' || r == '/' || r == '{' || r == '}':
			flush()
			prevLower = false
		case unicode.IsUpper(r):
			if prevLower {
				flush()
			}
			cur = append(cur, r)
			prevLower = false
		default:
			cur = append(cur, r)
			prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
		}
	}
	flush()
	return words
}

// commonInitialisms are upper-cased wholesale to match Go conventions.
var commonInitialisms = map[string]bool{
	"id": true, "url": true, "uri": true, "api": true, "http": true,
	"https": true, "json": true, "xml": true, "html": true, "sql": true,
	"uuid": true, "ip": true, "cpu": true, "gpu": true, "ttl": true,
}

// pascalCase produces an exported Go identifier (e.g. "petId" -> "PetID").
func pascalCase(s string) string {
	words := splitWords(s)
	var b strings.Builder
	for _, w := range words {
		if commonInitialisms[strings.ToLower(w)] {
			b.WriteString(strings.ToUpper(w))
			continue
		}
		b.WriteString(capitalizeASCII(strings.ToLower(w)))
	}
	out := b.String()
	if out == "" {
		return "Field"
	}
	if unicode.IsDigit(rune(out[0])) {
		return "F" + out
	}
	return out
}

// camelCase produces a lowerCamelCase identifier (used for TS/Go method args).
func camelCase(s string) string {
	p := pascalCase(s)
	if p == "" {
		return p
	}
	r := []rune(p)
	// Lowercase the leading run of upper-case letters up to the last one before
	// a lower-case letter, matching typical camelCase of initialisms.
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	if i == 0 {
		return p
	}
	if i == len(r) {
		return strings.ToLower(p)
	}
	if i > 1 {
		i--
	}
	for j := 0; j < i; j++ {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}

// snakeCase produces a snake_case identifier (used for Python).
func snakeCase(s string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	if len(words) == 0 {
		return "field"
	}
	return strings.Join(words, "_")
}
