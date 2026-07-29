package detect

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rule is a parsed Sigma rule. Only the fields the engine needs are modeled; the
// raw detection block is kept as a generic map so any Sigma search structure can
// be evaluated.
type Rule struct {
	Title       string         `yaml:"title" json:"title"`
	ID          string         `yaml:"id" json:"id"`
	Status      string         `yaml:"status" json:"status,omitempty"`
	Description string         `yaml:"description" json:"description,omitempty"`
	Level       string         `yaml:"level" json:"level"` // informational|low|medium|high|critical
	Tags        []string       `yaml:"tags" json:"tags,omitempty"`
	LogSource   LogSource      `yaml:"logsource" json:"logsource"`
	Detection   map[string]any `yaml:"detection" json:"-"`

	condition condExpr // parsed from Detection["condition"]
	searchIDs []string // detection keys except "condition"
}

// LogSource identifies the telemetry a rule applies to (Sigma logsource).
type LogSource struct {
	Category string `yaml:"category" json:"category,omitempty"`
	Product  string `yaml:"product" json:"product,omitempty"`
	Service  string `yaml:"service" json:"service,omitempty"`
}

// Technique returns the first MITRE ATT&CK technique id from the rule tags
// (e.g. tag "attack.t1059.001" → "T1059.001"), or "" when none is tagged.
func (r *Rule) Technique() string {
	for _, t := range r.Tags {
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "attack.t") {
			id := strings.TrimPrefix(low, "attack.")
			return strings.ToUpper(id)
		}
	}
	return ""
}

// ParseRule parses a single Sigma rule from YAML and compiles its condition.
func ParseRule(data []byte) (*Rule, error) {
	var r Rule
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("sigma: parse yaml: %w", err)
	}
	if r.Title == "" {
		return nil, fmt.Errorf("sigma: rule missing title")
	}
	if r.Detection == nil {
		return nil, fmt.Errorf("sigma: rule %q missing detection", r.Title)
	}
	condRaw, ok := r.Detection["condition"]
	if !ok {
		return nil, fmt.Errorf("sigma: rule %q missing detection.condition", r.Title)
	}
	condStr, ok := condRaw.(string)
	if !ok {
		return nil, fmt.Errorf("sigma: rule %q condition must be a string", r.Title)
	}
	cond, err := parseCondition(condStr)
	if err != nil {
		return nil, fmt.Errorf("sigma: rule %q: %w", r.Title, err)
	}
	r.condition = cond
	for k := range r.Detection {
		if k != "condition" {
			r.searchIDs = append(r.searchIDs, k)
		}
	}
	if len(r.searchIDs) == 0 {
		return nil, fmt.Errorf("sigma: rule %q has no search identifiers", r.Title)
	}
	return &r, nil
}

// Matches reports whether the rule fires for the given event. The event is a
// flat field→value map (values may be string/number/bool). Matching is
// case-insensitive for string comparisons, per common Sigma backend behavior.
func (r *Rule) Matches(event map[string]any) bool {
	matched := make(map[string]bool, len(r.searchIDs))
	for _, id := range r.searchIDs {
		matched[id] = matchSearch(r.Detection[id], event)
	}
	return r.condition.eval(matched, r.searchIDs)
}

// matchSearch evaluates one search identifier against the event. A map is an AND
// over its fields; a list is an OR over its items (each a map = AND, or a bare
// scalar = keyword search over all event values).
func matchSearch(spec any, event map[string]any) bool {
	switch v := spec.(type) {
	case map[string]any:
		return matchFieldMap(v, event)
	case []any:
		for _, item := range v {
			switch it := item.(type) {
			case map[string]any:
				if matchFieldMap(it, event) {
					return true
				}
			default:
				if keywordMatch(toString(it), event) {
					return true
				}
			}
		}
		return false
	default:
		// A bare scalar identifier is treated as a single keyword.
		return keywordMatch(toString(v), event)
	}
}

// matchFieldMap requires every "field[|modifiers]: value" entry to match (AND).
func matchFieldMap(fields map[string]any, event map[string]any) bool {
	for key, want := range fields {
		if !matchField(key, want, event) {
			return false
		}
	}
	return true
}

// matchField matches one field entry, honoring Sigma modifiers after '|'.
func matchField(key string, want any, event map[string]any) bool {
	parts := strings.Split(key, "|")
	field := parts[0]
	mods := parts[1:]

	requireAll := false
	for _, m := range mods {
		if strings.EqualFold(m, "all") {
			requireAll = true
		}
	}

	evVal, present := event[field]
	// Build the candidate value list (OR by default, AND when |all).
	wants := toList(want)
	if len(wants) == 0 {
		// Explicit null match: field must be absent or empty.
		return !present || toString(evVal) == ""
	}

	evStr := toString(evVal)
	match := func(w any) bool { return applyModifiers(mods, evStr, evVal, toString(w), present) }

	if requireAll {
		for _, w := range wants {
			if !match(w) {
				return false
			}
		}
		return true
	}
	for _, w := range wants {
		if match(w) {
			return true
		}
	}
	return false
}

// applyModifiers compares one wanted value against the event value under the
// active Sigma modifiers (contains/startswith/endswith/re/cidr; default equals).
func applyModifiers(mods []string, evStr string, evVal any, want string, present bool) bool {
	op := ""
	for _, m := range mods {
		switch strings.ToLower(m) {
		case "contains", "startswith", "endswith", "re", "cidr":
			op = strings.ToLower(m)
		}
	}
	switch op {
	case "contains":
		return present && strings.Contains(strings.ToLower(evStr), strings.ToLower(want))
	case "startswith":
		return present && strings.HasPrefix(strings.ToLower(evStr), strings.ToLower(want))
	case "endswith":
		return present && strings.HasSuffix(strings.ToLower(evStr), strings.ToLower(want))
	case "re":
		if !present {
			return false
		}
		re, err := regexp.Compile(want)
		if err != nil {
			return false
		}
		return re.MatchString(evStr)
	case "cidr":
		return present && cidrContains(want, evStr)
	default:
		return present && strings.EqualFold(evStr, want)
	}
}

// cidrContains reports whether ip falls inside the CIDR range.
func cidrContains(cidr, ip string) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	parsed := net.ParseIP(ip)
	return parsed != nil && network.Contains(parsed)
}

// keywordMatch reports whether kw appears (case-insensitive substring) in any of
// the event's string-rendered values.
func keywordMatch(kw string, event map[string]any) bool {
	if kw == "" {
		return false
	}
	low := strings.ToLower(kw)
	for _, v := range event {
		if strings.Contains(strings.ToLower(toString(v)), low) {
			return true
		}
	}
	return false
}

// toList normalizes a wanted value into a slice (a single scalar → one element).
func toList(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	default:
		return []any{x}
	}
}

// toString renders an event/rule value as a string for comparison.
func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		return x.String()
	case float64:
		// JSON/YAML numbers: render integers without a trailing ".0".
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
