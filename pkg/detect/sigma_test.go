package detect

import "testing"

// ---- condition grammar ----

func evalCond(t *testing.T, cond string, matched map[string]bool, ids []string) bool {
	t.Helper()
	expr, err := parseCondition(cond)
	if err != nil {
		t.Fatalf("parse %q: %v", cond, err)
	}
	return expr.eval(matched, ids)
}

func TestCondition_Grammar(t *testing.T) {
	ids := []string{"selection", "selection_1", "selection_2", "filter"}
	cases := []struct {
		cond    string
		matched map[string]bool
		want    bool
	}{
		{"selection", map[string]bool{"selection": true}, true},
		{"selection", map[string]bool{"selection": false}, false},
		{"selection and not filter", map[string]bool{"selection": true, "filter": false}, true},
		{"selection and not filter", map[string]bool{"selection": true, "filter": true}, false},
		{"selection or filter", map[string]bool{"selection": false, "filter": true}, true},
		{"(selection or filter) and not selection_1", map[string]bool{"filter": true, "selection_1": false}, true},
		{"1 of selection_*", map[string]bool{"selection_1": true}, true},
		{"1 of selection_*", map[string]bool{"selection_1": false, "selection_2": false}, false},
		{"all of selection_*", map[string]bool{"selection_1": true, "selection_2": true}, true},
		{"all of selection_*", map[string]bool{"selection_1": true, "selection_2": false}, false},
		{"any of them", map[string]bool{"filter": true}, true},
		{"all of them", map[string]bool{"selection": true, "selection_1": true, "selection_2": true, "filter": true}, true},
		{"2 of selection_*", map[string]bool{"selection_1": true, "selection_2": true}, true},
		{"2 of selection_*", map[string]bool{"selection_1": true, "selection_2": false}, false},
		// precedence: not > and > or
		{"selection_1 or selection_2 and filter", map[string]bool{"selection_1": true, "selection_2": true, "filter": false}, true},
	}
	for _, c := range cases {
		if got := evalCond(t, c.cond, c.matched, ids); got != c.want {
			t.Errorf("cond %q matched=%v: got %v want %v", c.cond, c.matched, got, c.want)
		}
	}
}

func TestCondition_ParseErrors(t *testing.T) {
	for _, bad := range []string{"", "selection and", "1 of", "(selection", "of them", "@bad"} {
		if _, err := parseCondition(bad); err == nil {
			t.Errorf("expected parse error for %q", bad)
		}
	}
}

// ---- field modifiers ----

func TestMatchField_Modifiers(t *testing.T) {
	ev := map[string]any{
		"Image":       `C:\Windows\System32\whoami.exe`,
		"CommandLine": "powershell -EncodedCommand ZQBjAGgAbwA=",
		"Dst":         "8.8.8.8",
		"Port":        4444,
	}
	cases := []struct {
		key  string
		want any
		ok   bool
	}{
		{"Image|endswith", `\whoami.exe`, true},
		{"Image|endswith", `\cmd.exe`, false},
		{"Image|startswith", `C:\Windows`, true},
		{"CommandLine|contains", "encodedcommand", true}, // case-insensitive
		{"CommandLine|re", `-Enc[a-zA-Z]+Command`, true},
		{"Dst|cidr", "8.8.8.0/24", true},
		{"Dst|cidr", "10.0.0.0/8", false},
		{"Port", "4444", true}, // numeric event value vs string want
		{"Image", "MISSINGVAL", false},
		{"Absent|contains", "x", false}, // absent field never matches contains
	}
	for _, c := range cases {
		if got := matchField(c.key, c.want, ev); got != c.ok {
			t.Errorf("matchField(%q,%v): got %v want %v", c.key, c.want, got, c.ok)
		}
	}
}

func TestMatchField_ListSemantics(t *testing.T) {
	ev := map[string]any{"CommandLine": "connect subprocess socket"}
	// default list = OR
	if !matchField("CommandLine|contains", []any{"nope", "socket"}, ev) {
		t.Errorf("OR list should match when any element matches")
	}
	// |all = AND
	if !matchField("CommandLine|contains|all", []any{"connect", "subprocess"}, ev) {
		t.Errorf("|all list should match when all elements present")
	}
	if matchField("CommandLine|contains|all", []any{"connect", "MISSING"}, ev) {
		t.Errorf("|all list must fail when one element is absent")
	}
}

// ---- rule parsing + Technique extraction ----

func TestParseRule_TechniqueAndErrors(t *testing.T) {
	valid := []byte(`title: X
level: high
tags: [attack.execution, attack.t1059.001]
logsource: {category: process_creation}
detection:
  selection:
    Image|endswith: '\x.exe'
  condition: selection
`)
	r, err := ParseRule(valid)
	if err != nil {
		t.Fatalf("parse valid: %v", err)
	}
	if r.Technique() != "T1059.001" {
		t.Errorf("technique: got %q want T1059.001", r.Technique())
	}
	// missing condition
	if _, err := ParseRule([]byte("title: Y\ndetection:\n  selection:\n    A: b\n")); err == nil {
		t.Errorf("expected error for missing condition")
	}
	// missing title
	if _, err := ParseRule([]byte("detection:\n  condition: selection\n")); err == nil {
		t.Errorf("expected error for missing title")
	}
}
