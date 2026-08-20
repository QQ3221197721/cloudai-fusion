package security

import (
	"fmt"
	"strings"
)

// ============================================================================
// Aho-Corasick Automaton - O(N+M+Z) Multi-Pattern Matching Engine
// ============================================================================

// ACPattern represents a searchable pattern with metadata.
type ACPattern struct {
	Pattern  string `json:"pattern"`   // literal pattern to match (lowercase)
	Category string `json:"category"`  // sqli, xss, path_traversal, rce, ssrf
	Security string `json:"severity"`  // critical, high, medium, low
	ID       string `json:"id"`        // unique pattern id
}

// ACMatch represents a detected pattern match in text.
type ACMatch struct {
	Pattern ACPattern `json:"pattern"`
	From    int       `json:"from"`    // start position in input text
	To      int       `json:"to"`      // end position (exclusive)
}

// acNode is a single node in the Aho-Corasick trie.
//
// children is a dense 256-way byte-indexed array rather than a map[byte]*acNode:
// the search hot loop then follows goto/fail edges with a single array index and
// no hashing, which is the dominant cost in multi-pattern matching over long text.
type acNode struct {
	children [256]*acNode // goto function: byte -> next node (dense, hash-free)
	output   []int        // indices into patterns slice where patterns end (direct)
	fail     *acNode      // failure link for fallback transitions
	failOut  []int        // precomputed union of outputs from the failure chain
	out      []int        // merged output ∪ failOut: the single list Search emits
	depth    int          // depth from root for tie-breaking
}

// AhoCorasick implements the Aho-Corasick multi-pattern matching automaton.
// All searches are case-insensitive by working on lowercase inputs.
type AhoCorasick struct {
	root      *acNode
	patterns  []ACPattern
	built     bool
	mismatch  int // mismatch counter (for analysis only)
}

// NewAhoCorasick creates an empty Aho-Corasick automaton.
func NewAhoCorasick() *AhoCorasick {
	return &AhoCorasick{
		root: &acNode{
			output: nil,
			fail:   nil, // points to root after Build()
			depth:  0,
		},
		patterns: make([]ACPattern, 0),
		built:    false,
		mismatch: 0,
	}
}

// AddPattern adds a pattern to the automaton's trie and lists it in patterns.
// Patterns must be registered before Build() constructs failure/output links.
func (ac *AhoCorasick) AddPattern(p ACPattern) {
	if p.Pattern == "" {
		return
	}
	idx := len(ac.patterns)
	ac.patterns = append(ac.patterns, p)

	// Insert pattern into the trie, lowercased for case-insensitive search
	node := ac.root
	for i := 0; i < len(p.Pattern); i++ {
		b := acLowerByte(p.Pattern[i])
		if node.children[b] == nil {
			node.children[b] = &acNode{
				output: nil,
				fail:   nil,
				depth:  node.depth + 1,
			}
		}
		node = node.children[b]
	}
	// Mark pattern index at the end node (output function)
	node.output = append(node.output, idx)
}

// acLowerByte performs ASCII lowercasing so matching is case-insensitive while
// preserving byte length (positions in original text stay valid).
func acLowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}

// AddPatterns批量添加模式，构建自动机。
func (ac *AhoCorasick) AddPatterns(patternsList []ACPattern) {
	for _, p := range patternsList {
		ac.AddPattern(p)
	}
}

// Build constructs the failure and output-link structures using BFS.
// After Build(), Search can be called.
func (ac *AhoCorasick) Build() {
	if len(ac.patterns) == 0 {
		ac.built = true
		return
	}

	root := ac.root
	queue := make([]*acNode, 0, 64)

	// Initialize depth-1 nodes: fail links point to root, no fail-chain output.
	// Their emit list is simply their own direct output.
	for b := 0; b < 256; b++ {
		node := root.children[b]
		if node == nil {
			continue
		}
		node.fail = root
		node.failOut = nil // inherits nothing from root's fail chain
		node.out = node.output
		queue = append(queue, node)
	}

	// BFS to construct failure links and propagate outputs
	head := 0
	for head < len(queue) {
		cur := queue[head]
		head++

		// Process each present child
		for b := 0; b < 256; b++ {
			child := cur.children[b]
			if child == nil {
				continue
			}
			bb := byte(b)
			// Find failure link: longest proper suffix
			f := cur.fail
			for f != nil && f.children[bb] == nil {
				f = f.fail
			}
			if f != nil && f.children[bb] != nil {
				child.fail = f.children[bb]
			} else {
				child.fail = root
			}

			// Precompute merge of output from fail chain (output links)
			failAsOutput := child.fail
			var failChain []int
			// Copy fail node's accumulated fail-chain output
			if failAsOutput != nil && failAsOutput.depth > 0 { // avoid copying from root
				failChain = append(failChain, failAsOutput.failOut...)
			}
			// Append direct outputs from failure node itself
			if failAsOutput != nil && failAsOutput.output != nil {
				failChain = append(failChain, failAsOutput.output...)
			}
			child.failOut = failChain

			// Merge direct output + fail-chain output into one flat emit list so
			// Search walks a single slice per position instead of two. Order
			// (direct first, then fail chain) matches the pre-merge Search output.
			if len(failChain) == 0 {
				child.out = child.output
			} else if len(child.output) == 0 {
				child.out = failChain
			} else {
				merged := make([]int, 0, len(child.output)+len(failChain))
				merged = append(merged, child.output...)
				merged = append(merged, failChain...)
				child.out = merged
			}

			queue = append(queue, child)
		}
	}

	ac.built = true
}

// Search performs O(N + M + Z) Aho-Corasick matching over input text.
// Returns all matches with positions; callers get (pattern, from, to).
func (ac *AhoCorasick) Search(text string) []ACMatch {
	if len(ac.patterns) == 0 {
		return nil
	}
	if !ac.built {
		ac.Build()
	}

	result := make([]ACMatch, 0, 8)
	cur := ac.root

	// Scan text left-to-right, following goto/fail edges
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])

		// Follow fail links until we find a goto edge or reach root
		// Hot loop now uses dense array child access for hash-free O(1) lookup.
		for cur.children[b] == nil && cur != ac.root {
			ac.mismatch++
			cur = cur.fail
		}
		if cur.children[b] != nil {
			cur = cur.children[b]
		}

		// Output matches from precomputed merged list in one pass (order unchanged).
		for _, pi := range cur.out {
			if pi >= 0 && pi < len(ac.patterns) {
				pat := ac.patterns[pi]
				l := len(pat.Pattern)
				if i+1 >= l {
					result = append(result, ACMatch{
						Pattern: pat,
						From:    i + 1 - l,
						To:      i + 1,
					})
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// SearchBytes matches over []byte instead of string. Same semantics as Search.
func (ac *AhoCorasick) SearchBytes(data []byte) []ACMatch {
	if len(ac.patterns) == 0 {
		return nil
	}
	if !ac.built {
		ac.Build()
	}

	result := make([]ACMatch, 0, 8)
	cur := ac.root

	for i := 0; i < len(data); i++ {
		b := acLowerByte(data[i])
		for cur.children[b] == nil && cur != ac.root {
			ac.mismatch++
			cur = cur.fail
		}
		if cur.children[b] != nil {
			cur = cur.children[b]
		}

		// Emit matches from the merged output list
		for _, pi := range cur.out {
			if pi >= 0 && pi < len(ac.patterns) {
				pat := ac.patterns[pi]
				l := len(pat.Pattern)
				if i+1 >= l {
					result = append(result, ACMatch{
						Pattern: pat,
						From:    i + 1 - l,
						To:      i + 1,
					})
				}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// VisitMatches is an output visitor invoked for every AC pattern hit.
type VisitMatches func(m ACMatch)

// SearchInto visits matches found in text using the provided visitor, avoiding
// allocation of the result slice when only detection/counts are needed.
func (ac *AhoCorasick) SearchInto(text string, v VisitMatches) {
	if len(ac.patterns) == 0 {
		return
	}
	if !ac.built {
		ac.Build()
	}

	cur := ac.root
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])
		for cur.children[b] == nil && cur != ac.root {
			cur = cur.fail
		}
		if cur.children[b] != nil {
			cur = cur.children[b]
		}

		// Emit matches from the merged output list
		for _, pi := range cur.out {
			if pi >= 0 && pi < len(ac.patterns) {
				pat := ac.patterns[pi]
				l := len(pat.Pattern)
				if i+1 >= l {
					v(ACMatch{
						Pattern: pat,
						From:    i + 1 - l,
						To:      i + 1,
					})
				}
			}
		}
	}
}

// MatchAny returns true if any of the patterns match the input (no allocations).
func (ac *AhoCorasick) MatchAny(text string) bool {
	if len(ac.patterns) == 0 {
		return false
	}
	if !ac.built {
		ac.Build()
	}

	found := false
	cur := ac.root
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])
		for cur.children[b] == nil && cur != ac.root {
			cur = cur.fail
		}
		if cur.children[b] != nil {
			cur = cur.children[b]
		}

		hasOutput := len(cur.out) > 0
		if hasOutput {
			// Fast exit: we know there's at least one match
			found = true
			break
		}
	}
	return found
}

// LenPatterns returns total pattern count registered.
func (ac *AhoCorasick) LenPatterns() int { return len(ac.patterns) }

// IsBuilt reports whether Build() has been called.
func (ac *AhoCorasick) IsBuilt() bool { return ac.built }

// Mismatches returns the cumulative number of fail-link traversals since last Search.
func (ac *AhoCorasick) Mismatches() int { return ac.mismatch }

// ResetMismatch clears the mismatch counter.
func (ac *AhoCorasick) ResetMismatch() { ac.mismatch = 0 }

// ============================================================================
// Default Attack Pattern Libraries (Real OWASP-inspired patterns)
// ============================================================================

// DefaultWAFPatterns returns a curated set of literal attack signatures covering major vulnerability classes.
// These are plain strings designed to work efficiently with Aho-Corasick matching.
func DefaultWAFPatterns() []ACPattern {
	pats := make([]ACPattern, 0, 320)
	add := func(list []string, cat, sev, prefix string) {
		for i, p := range list {
			pats = append(pats, ACPattern{Pattern: p, Category: cat, Security: sev, ID: fmt.Sprintf("%s-%d", prefix, i)})
		}
	}

	// --- SQL Injection ---
	add([]string{
		"' or '1'='1", "' or 1=1", "' or 1=1--", "' or 'a'='a", "\" or \"1\"=\"1", "or 1=1",
		"1' and '1'='1", "1' and '1'='2", "admin'--", "admin' #", "' or ''='",
		"union select", "union all select", "select * from", "select from",
		"drop table", "drop database", "insert into", "update set", "update sets",
		"delete from", "alter table", "create table", "truncate table",
		"--", "/**/", "/* */", "'--", "; --", "; drop", "; insert", "; update", "; delete",
		"waitfor delay", "benchmark(", "sleep(", "pg_sleep", "and sleep", "or sleep",
		"char(", "concat(", "concat_ws(", "substring(", "substr(", "mid(",
		"cast(", "convert(", "binary(", "hex(", "unhex(", "ascii(",
		"having 1=1", "group by", "order by", "into outfile", "into dumpfile",
		"load_file(", "write_file(", "xp_cmdshell", "sp_executesql",
		"@@version", "@@servername", "@@datadir", "version()", "database()",
		"user()", "current_user", "session_user", "system_user",
		"information_schema", "sysobjects", "syscolumns", "sys.tables",
		"table_name", "column_name", "extractvalue(", "updatexml(", "floor(rand",
		"rlike ", " regexp ", "like 0x", "and 1=1", "and 1=2", "'; exec", "'; waitfor",
		"utl_http", "dbms_pipe", "declare @", "nchar(", "nvarchar(", "0x",
	}, "sqli", "critical", "sql")

	// --- XSS ---
	add([]string{
		"<script", "</script", "<img", "<svg", "<body", "<iframe", "</iframe",
		"<object", "</object", "<embed", "<marquee", "<applet", "<base", "<meta",
		"<form", "<input", "<textarea", "<video", "<audio", "<details", "<link",
		"javascript:", "vbscript:", "livescript:", "data:text/html",
		"onerror=", "onload=", "onmouseover=", "onfocus=", "onblur=", "onclick=",
		"onsubmit=", "onkeyup=", "onkeydown=", "onkeypress=", "onmouseout=",
		"onmousedown=", "onmouseup=", "onchange=", "ondblclick=", "oncontextmenu=",
		"onwheel=", "ondrag=", "onscroll=", "onanimationstart=", "ontoggle=",
		"eval(", "document.cookie", "document.write", "innerhtml", "outerhtml",
		"string.fromcharcode", "fromcharcode", "alert(", "prompt(", "confirm(",
		"settimeout(", "setinterval(", "atob(", "expression(", "binding(",
		"&#x", "&#0", "&lt;script",
	}, "xss", "high", "xss")

	// --- Path Traversal ---
	add([]string{
		"../", "..\\", "....//", "...\\\\", "..;/", "..%2f", "..%5c",
		"%2e%2e%2f", "%2e%2e/", "%2e%2e%5c", "%252e%252e%252f", "..%252f",
		"..%252f%252e%252e%252f", "%c0%ae", "%uff0e", "....\\.",
		"/etc/passwd", "/etc/shadow", "/etc/hosts", "/etc/group", "/proc/self",
		"/proc/version", "/var/log", "/etc/passwd%00",
		"c:\\windows", "c:/windows", "c:\\boot.ini", "c:/boot.ini",
		"/windows/system32", "web.config", "wp-config.php", ".ssh/",
	}, "path_traversal", "high", "pt")

	// --- Command Injection ---
	add([]string{
		"; ls ", "; cat ", "; rm ", "; pwd", "; whoami", "; id", "; uname",
		"| ls ", "| cat ", "| id", "| whoami", "&& rm ", "|| cat", "&& cat",
		"; nc ", "| nc ", "; curl ", "| curl ", "; wget ", "| wget ",
		"&& ping", "; ping", "; kill", "; bash", "; sh ", "; python", "; perl",
		"$(", "`", "<(", "${ifs}", "$ifs", "chmod +x", "2>&1", "> /dev/null",
		"; exec ", "system(", "popen(", "shell_exec(", "passthru(", "proc_open(",
		"/bin/sh", "/bin/bash", "/usr/bin/perl", "/usr/bin/python",
		"?cmd=", "&cmd=", "?command=", "&command=", "; powershell", "cmd.exe",
	}, "rce", "critical", "rce")

	// --- SSRF ---
	add([]string{
		"169.254.169.254", "169.254.170.2", "metadata.google", "metadata.azure",
		"localhost", "127.0.0.1", "::1", "[::1]", "0.0.0.0", "0177.0.0.1",
		"2130706433", "0x7f000001", "internal.", "intranet", ".local",
		"http://127", "http://localhost", "http://0x", "http://[::",
		"@127.0.0.1", "@localhost", "file://", "dict://", "gopher://",
		"ftp://127", "tftp://", "ldap://", "jar://",
	}, "ssrf", "high", "ssrf")

	// --- Scanners/Bots ---
	add([]string{
		"sqlmap", "nikto", "nessus", "acunetix", "nmap",
		"masscan", "gobuster", "dirbuster", "wfuzz", "burpsuite",
		"owasp", "arachni", "w3af", "skipfish", "hydra",
	}, "scanner", "medium", "scan")

	// --- Sensitive Files/Paths ---
	add([]string{
		".env", ".git", ".svn", ".htaccess", ".htpasswd",
		".bak", ".sql", ".dump", ".pem", ".key",
		"/wp-admin", "/phpmyadmin", "/admin.php", "/console",
		"/actuator", "/status", "/metrics", "/.aws/credentials",
		"id_rsa", "id_dsa", "known_hosts", "authorized_keys",
		"docker-compose", "kubeconfig", "credentials.json",
	}, "sensitive", "medium", "sens")

	return pats
}

// NormalizeToLower converts text to lowercase for case-insensitive matching.
func NormalizeToLower(s string) string {
	// Use strings.ToLower for ASCII-compatible case folding
	return strings.ToLower(s)
}
