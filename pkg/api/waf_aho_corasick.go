// Package api - Aho-Corasick Multi-Pattern WAF Engine
//
// Performance Barrier: O(text_len) single-pass matching for ALL rules simultaneously
//
// Competitive Baseline: ModSecurity/traditional WAF uses per-rule regex matching.
// With N rules, each request costs O(text_len * N). At 1000 rules + 1KB request body,
// that's 1M character comparisons per request.
//
// Our Innovation: Compile all WAF patterns into a single Aho-Corasick automaton.
// Single pass over request body matches ALL patterns simultaneously.
// Complexity: O(text_len + matches) regardless of number of rules.
//
// Additional: IP blocklist uses radix tree for O(prefix_len) CIDR lookup,
// independent of blocklist size.
package api

import "sync"

// AhoCorasickMatcher implements the Aho-Corasick multi-pattern string matching algorithm.
// All WAF rules are compiled into a single automaton at startup.
// Runtime matching is O(text_length) regardless of pattern count.
type AhoCorasickMatcher struct {
	root     *acNode
	patterns []acPattern
	compiled bool
	mu       sync.RWMutex
}

type acNode struct {
	children map[byte]*acNode
	fail     *acNode
	output   []int // indices of matching patterns at this node
	depth    int
}

type acPattern struct {
	ID       string
	Pattern  []byte
	Severity string // critical, high, medium, low
}

// NewAhoCorasickMatcher creates a new AC automaton.
func NewAhoCorasickMatcher() *AhoCorasickMatcher {
	return &AhoCorasickMatcher{
		root: &acNode{children: make(map[byte]*acNode)},
	}
}

// AddPattern adds a WAF pattern to be matched.
// Must call Compile() after adding all patterns.
func (ac *AhoCorasickMatcher) AddPattern(id string, pattern []byte, severity string) {
	ac.patterns = append(ac.patterns, acPattern{ID: id, Pattern: pattern, Severity: severity})
}

// Compile builds the Aho-Corasick automaton (goto + failure + output functions).
// Called once at startup. Complexity: O(sum of all pattern lengths).
func (ac *AhoCorasickMatcher) Compile() {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	// Build goto function (trie)
	for idx, p := range ac.patterns {
		node := ac.root
		for _, b := range p.Pattern {
			if node.children[b] == nil {
				node.children[b] = &acNode{children: make(map[byte]*acNode), depth: node.depth + 1}
			}
			node = node.children[b]
		}
		node.output = append(node.output, idx)
	}

	// Build failure function (BFS)
	queue := make([]*acNode, 0, 256)
	for _, child := range ac.root.children {
		child.fail = ac.root
		queue = append(queue, child)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for b, child := range current.children {
			queue = append(queue, child)

			// Find failure state
			fail := current.fail
			for fail != nil && fail.children[b] == nil {
				fail = fail.fail
			}
			if fail == nil {
				child.fail = ac.root
			} else {
				child.fail = fail.children[b]
			}

			// Merge output from failure chain
			if child.fail != nil {
				child.output = append(child.output, child.fail.output...)
			}
		}
	}

	ac.compiled = true
}

// WAFMatch holds info about a matched pattern.
type WAFMatch struct {
	PatternID string
	Position  int
	Severity  string
}

// Match scans text against ALL compiled patterns in a single pass.
// Complexity: O(text_len + number_of_matches) — independent of pattern count!
// This is the core performance advantage over per-rule regex scanning.
func (ac *AhoCorasickMatcher) Match(text []byte) []WAFMatch {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	if !ac.compiled || len(text) == 0 {
		return nil
	}

	var matches []WAFMatch
	node := ac.root

	for pos, b := range text {
		// Follow failure links until we find a transition or reach root
		for node != ac.root && node.children[b] == nil {
			node = node.fail
		}
		if next := node.children[b]; next != nil {
			node = next
		}

		// Check outputs at current state
		if len(node.output) > 0 {
			for _, idx := range node.output {
				p := ac.patterns[idx]
				matches = append(matches, WAFMatch{
					PatternID: p.ID,
					Position:  pos - len(p.Pattern) + 1,
					Severity:  p.Severity,
				})
			}
		}
	}

	return matches
}

// HasMatch returns true if any pattern matches (early termination).
// Faster than Match() when you only need a yes/no answer.
func (ac *AhoCorasickMatcher) HasMatch(text []byte) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	if !ac.compiled || len(text) == 0 {
		return false
	}

	node := ac.root
	for _, b := range text {
		for node != ac.root && node.children[b] == nil {
			node = node.fail
		}
		if next := node.children[b]; next != nil {
			node = next
		}
		if len(node.output) > 0 {
			return true // early termination on first match
		}
	}
	return false
}

// PatternCount returns number of compiled patterns.
func (ac *AhoCorasickMatcher) PatternCount() int {
	return len(ac.patterns)
}

// --- Standard WAF rules pre-loaded ---

// DefaultWAFPatterns returns common SQL injection, XSS, command injection patterns.
func DefaultWAFPatterns() []acPattern {
	return []acPattern{
		// SQL Injection
		{ID: "sqli-union", Pattern: []byte("UNION SELECT"), Severity: "critical"},
		{ID: "sqli-or-1", Pattern: []byte("' OR 1=1"), Severity: "critical"},
		{ID: "sqli-or-true", Pattern: []byte("' OR 'a'='a"), Severity: "critical"},
		{ID: "sqli-drop", Pattern: []byte("DROP TABLE"), Severity: "critical"},
		{ID: "sqli-semicolon", Pattern: []byte("; DELETE"), Severity: "high"},
		{ID: "sqli-comment", Pattern: []byte("--"), Severity: "medium"},
		{ID: "sqli-xp-cmd", Pattern: []byte("xp_cmdshell"), Severity: "critical"},
		// XSS
		{ID: "xss-script", Pattern: []byte("<script"), Severity: "high"},
		{ID: "xss-onerror", Pattern: []byte("onerror="), Severity: "high"},
		{ID: "xss-onload", Pattern: []byte("onload="), Severity: "high"},
		{ID: "xss-alert", Pattern: []byte("alert("), Severity: "medium"},
		{ID: "xss-eval", Pattern: []byte("eval("), Severity: "high"},
		// Command Injection
		{ID: "cmdi-pipe", Pattern: []byte("| /bin/"), Severity: "critical"},
		{ID: "cmdi-backtick", Pattern: []byte("`;"), Severity: "high"},
		{ID: "cmdi-etc-passwd", Pattern: []byte("/etc/passwd"), Severity: "critical"},
		{ID: "cmdi-etc-shadow", Pattern: []byte("/etc/shadow"), Severity: "critical"},
		// Path Traversal
		{ID: "path-dotdot", Pattern: []byte("../"), Severity: "high"},
		{ID: "path-dotdot-win", Pattern: []byte("..\\"), Severity: "high"},
		// Scanner Detection
		{ID: "scanner-sqlmap", Pattern: []byte("sqlmap"), Severity: "medium"},
		{ID: "scanner-nikto", Pattern: []byte("Nikto"), Severity: "medium"},
	}
}

// NewDefaultWAF creates a pre-compiled WAF with standard detection rules.
func NewDefaultWAF() *AhoCorasickMatcher {
	ac := NewAhoCorasickMatcher()
	for _, p := range DefaultWAFPatterns() {
		ac.AddPattern(p.ID, p.Pattern, p.Severity)
	}
	ac.Compile()
	return ac
}
