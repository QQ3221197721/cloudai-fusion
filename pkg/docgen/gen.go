// Package docgen generators: Markdown output for rendered docs.
package docgen

import (
	"bytes"
	"os"
	"path/filepath"
	"text/template"
	"time"
)

// Generator renders package docs to Markdown.
type Generator struct {
	Dir   string // output directory
	Title string // document title
}

// Generate creates one or more markdown files for a parsed package.
func (g *Generator) Generate(pkg *Package) error {
	if g.Dir == "" {
		g.Dir = "./docs"
	}
	if g.Title == "" {
		g.Title = pkg.Name + " Documentation"
	}

	os.MkdirAll(g.Dir, 0o755)

	indexTmpl := `# {{.Title}}
{{if .Description}}
{{.Description}}
{{end}}

## Symbols
{{range .AllSymbols}}{{with .}} - [{{.Name}}]{{if .Recv}}({{.Recv}}){{end}}
{{end}}{{end}}

Generated on {{.Date}}
`

	typesTmpl := `# {{.Title}} - Types
{{if .Description}}
{{.Description}}
{{end}}

## Type Definitions
### Functions
{{range .Funcs}}{{with .}}
#### {{.Name}}{{if .Recv}}({{.Recv}}){{end}}
{{.Signature}}
{{.Doc}}

{{end}}{{end}}

### Variables
{{range .Vars}}{{with .}}
#### {{.Name}}
{{.Signature}}
{{.Doc}}

{{end}}{{end}}

### Constants
{{range .Consts}}{{with .}}
#### {{.Name}}
{{.Signature}}
{{.Doc}}

{{end}}{{end}}

### Types
{{range .Types}}{{with .}}
#### {{.Name}}
{{.Decl}}
{{.Doc}}

{{if .Fields}}**Fields**:
{{range .Fields}}- {{.Name}} {{.Type}}: {{.Doc}}
{{end}}{{end}}
{{if .Methods}}**Methods**:
{{range .Methods}}- {{.Name}}: {{.Signature}}
{{end}}{{end}}
{{end}}{{end}}
`

	files := []struct {
		name    string
		content string
	}{
		{"index.md", execute(indexTmpl, map[string]any{"Title": g.Title, "Description": pkg.Doc, "AllSymbols": pkg.AllSymbols(), "Date": time.Now().Format("2006-01-02")})},
		{"types.md", execute(typesTmpl, map[string]any{"Title": g.Title + " - Types", "Funcs": pkg.Funcs, "Vars": pkg.Vars, "Consts": pkg.Consts, "Types": pkg.Types, "Date": time.Now().Format("2006-01-02")})},
	}

	for _, f := range files {
		path := filepath.Join(g.Dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func execute(tmpl string, data any) string {
	t := template.Must(template.New("md").Parse(tmpl))
	var b bytes.Buffer
	t.Execute(&b, data)
	return b.String()
}
