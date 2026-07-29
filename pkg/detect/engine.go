package detect

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed rules/*.yml
var embeddedRules embed.FS

// Match is a rule firing on an event, carrying the triage metadata callers need.
type Match struct {
	RuleID    string   `json:"rule_id"`
	Title     string   `json:"title"`
	Level     string   `json:"level"`
	Technique string   `json:"technique,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Category  string   `json:"category,omitempty"`
}

// Engine holds a compiled Sigma rule set and evaluates events against it.
type Engine struct {
	rules []*Rule
}

// NewEngine builds an engine over already-parsed rules.
func NewEngine(rules []*Rule) *Engine { return &Engine{rules: rules} }

// NewEmbeddedEngine loads the built-in Sigma rule set embedded in the binary.
// It is the honest default: real community-style detection content, no external
// dependency, always available in CI.
func NewEmbeddedEngine() (*Engine, error) {
	rules, err := loadFromFS(embeddedRules, "rules")
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, fmt.Errorf("sigma: no embedded rules found")
	}
	return &Engine{rules: rules}, nil
}

// LoadDir parses every *.yml/*.yaml Sigma rule under dir (using the given fs.FS,
// e.g. os.DirFS("/etc/cloudai/sigma")) and adds them to the engine. This is how
// an operator drops in the thousands of upstream SigmaHQ rules at deploy time.
func (e *Engine) LoadDir(fsys fs.FS, dir string) (int, error) {
	rules, err := loadFromFS(fsys, dir)
	if err != nil {
		return 0, err
	}
	e.rules = append(e.rules, rules...)
	return len(rules), nil
}

// loadFromFS walks dir in fsys and parses each YAML file as a Sigma rule.
func loadFromFS(fsys fs.FS, dir string) ([]*Rule, error) {
	var rules []*Rule
	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isYAML(path) {
			return nil
		}
		data, rerr := fs.ReadFile(fsys, path)
		if rerr != nil {
			return fmt.Errorf("sigma: read %s: %w", path, rerr)
		}
		rule, perr := ParseRule(data)
		if perr != nil {
			return fmt.Errorf("sigma: %s: %w", path, perr)
		}
		rules = append(rules, rule)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Title < rules[j].Title })
	return rules, nil
}

func isYAML(path string) bool {
	return strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")
}

// Eval runs every rule whose logsource category applies to the event's category
// (a rule with an empty category matches any) and returns all matches, ordered
// by descending severity level.
func (e *Engine) Eval(category string, event map[string]any) []Match {
	var out []Match
	for _, r := range e.rules {
		if r.LogSource.Category != "" && !strings.EqualFold(r.LogSource.Category, category) {
			continue
		}
		if r.Matches(event) {
			out = append(out, Match{
				RuleID:    r.ID,
				Title:     r.Title,
				Level:     r.Level,
				Technique: r.Technique(),
				Tags:      r.Tags,
				Category:  r.LogSource.Category,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return levelRank(out[i].Level) > levelRank(out[j].Level) })
	return out
}

// EvalBatch evaluates a batch of events, returning all matches across them.
func (e *Engine) EvalBatch(category string, events []map[string]any) []Match {
	var out []Match
	for _, ev := range events {
		out = append(out, e.Eval(category, ev)...)
	}
	return out
}

// Rules returns the loaded rules (read-only use).
func (e *Engine) Rules() []*Rule { return e.rules }

// Len returns the number of loaded rules.
func (e *Engine) Len() int { return len(e.rules) }

// levelRank orders Sigma severity levels for sorting.
func levelRank(level string) int {
	switch strings.ToLower(level) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "informational":
		return 1
	default:
		return 0
	}
}
