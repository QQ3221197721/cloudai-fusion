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
	fail     *acNode      // failure link for fallback transitions during build
	failOut  []int        // precomputed union of outputs from the failure chain
	out      []int        // merged output ∪ failOut: the single list Search emits
	depth    int          // depth from root for tie-breaking
	stateID  int32        // unique state ID assigned during Build() for DFA table indexing
}

// AhoCorasick implements the Aho-Corasick multi-pattern matching automaton.
// All searches are case-insensitive by working on lowercase inputs.
//
// Optimization (v2 with alphabet-reduced DFA): Uses a precomputed goto table
// for O(1) transitions. Each node state is assigned a unique ID during Build().
// Rather than a full 256-wide row per state (which for ~55k states is ~54 MB
// and thrashes cache), we exploit that any byte NOT present in any pattern can
// only ever transition back to root (state 0): such "dead" bytes share a single
// "other" column. The row width shrinks from 256 to (liveAlphabet+1), cutting
// the table ~4x so it stays cache-resident. The flat table maps
// (stateID, column) -> nextStateID via index = stateID*rowWidth + alphaMap[byte].
type AhoCorasick struct {
	root      *acNode
	patterns  []ACPattern
	built     bool
	mismatch  int        // mismatch counter (for analysis only)
	gotoTable []int32    // flattened DFA table: [numStates * rowWidth] entries
	stateOut  [][]int    // per-state merged emit list, indexed by stateID (== node.out)
	alphaMap  [256]int32 // byte -> column index; dead bytes map to the shared "other" column
	rowWidth  int32      // = liveAlphabetSize + 1 (last column is the "other"/dead column)
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
				output:  nil,
				fail:    nil,
				depth:   node.depth + 1,
				stateID: -1, // unassigned until Build() BFS numbers states
			}
		}
		node = node.children[b]
	}
	// Mark pattern index at the end node (output function)
	node.output = append(node.output, idx)
}

// collectNodesByStateID collects all nodes from the trie and assigns them to their state ID slot in outSlice.
// The slice must be preallocated to numStates length.
func collectNodesByStateID(node *acNode, outSlice []*acNode) {
	if node == nil {
		return
	}
	// Assign node to its precomputed state slot
	if node.stateID >= 0 && node.stateID < int32(len(outSlice)) {
		outSlice[node.stateID] = node
	}
	// DFS through all children
	for b := 0; b < 256; b++ {
		child := node.children[b]
		if child != nil {
			collectNodesByStateID(child, outSlice)
		}
	}
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

	// Initialize root state ID = 0, and all other nodes to -1 (unassigned)
	ac.root.stateID = 0
	stateCount := int32(1) // root is state 0, so start counting from 1

	// Initialize depth-1 nodes: fail links point to root, no fail-chain output.
	// Their emit list is simply their own direct output.
	for b := 0; b < 256; b++ {
		node := root.children[b]
		if node == nil {
			continue
		}
		node.fail = root
		node.stateID = stateCount
		stateCount++
		node.failOut = nil // inherits nothing from root's fail chain
		node.out = node.output
		queue = append(queue, node)
	}

	// BFS to construct failure links and propagate outputs
	head := 0
	for head < len(queue) {
		cur := queue[head]
		head++
		if cur.stateID == -1 {
			cur.stateID = stateCount
			stateCount++
		}

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

	// ============================================================
	// Post-BFS: Collect all states and build the alphabet-reduced DFA goto table
	// ============================================================
	numStates := int(stateCount)

	// First pass: collect all nodes by state ID into a slice
	allNodes := make([]*acNode, numStates)
	collectNodesByStateID(ac.root, allNodes)

	// Compute the live alphabet: a byte is "live" iff some trie node has an edge
	// on it (equivalently, it occurs in some pattern). Dead bytes can only ever
	// transition back to root, so they collapse into a single shared column.
	var liveByte [256]bool
	for _, n := range allNodes {
		for b := 0; b < 256; b++ {
			if n.children[b] != nil {
				liveByte[b] = true
			}
		}
	}
	// Assign columns: live bytes get 0..K-1 (in byte order); dead bytes all map
	// to the last "other" column K. rowWidth = K+1.
	var col int32
	for b := 0; b < 256; b++ {
		if liveByte[b] {
			ac.alphaMap[b] = col
			col++
		}
	}
	otherCol := col // shared column for every dead byte
	for b := 0; b < 256; b++ {
		if !liveByte[b] {
			ac.alphaMap[b] = otherCol
		}
	}
	ac.rowWidth = otherCol + 1
	rowWidth := int(ac.rowWidth)

	ac.gotoTable = make([]int32, numStates*rowWidth)
	ac.stateOut = make([][]int, numStates)

	// Root is state 0, with no direct outputs (we never match empty patterns)
	ac.stateOut[0] = nil

	// For each state, precompute the DFA transition function over the reduced
	// alphabet. The "other" column is left at its zero value (0 == root), which
	// is exactly correct: a dead byte always resets the automaton to root.
	for stID := 0; stID < numStates; stID++ {
		stateNode := allNodes[stID]
		base := stID * rowWidth

		for b := 0; b < 256; b++ {
			if !liveByte[b] {
				continue // dead byte -> handled by the shared "other" column (stays 0)
			}
			c := int(ac.alphaMap[b])

			// Hot path: dense array access for goto edge
			targetNode := stateNode.children[b]
			if targetNode != nil {
				ac.gotoTable[base+c] = targetNode.stateID
			} else {
				// Otherwise follow fail links like classical AC
				f := stateNode.fail
				for f != nil && f.children[b] == nil {
					f = f.fail
				}
				if f != nil && f.children[b] != nil {
					ac.gotoTable[base+c] = f.children[b].stateID
				} else {
					ac.gotoTable[base+c] = 0 // fall back to root
				}
			}
		}

		// Populate state output list
		if len(stateNode.out) > 0 {
			ac.stateOut[stID] = append([]int(nil), stateNode.out...)
		}
	}

	ac.built = true
}

// Search performs O(N + M + Z) Aho-Corasick matching over input text.
// Uses precomputed DFA goto table for O(1) transitions.
// Returns all matches with positions; callers get (pattern, from, to).
func (ac *AhoCorasick) Search(text string) []ACMatch {
	if len(ac.patterns) == 0 {
		return nil
	}
	if !ac.built {
		ac.Build()
	}

	result := make([]ACMatch, 0, 8)
	state := int32(0) // Start at root state 0
	rowWidth := int(ac.rowWidth)
	gotoTable := ac.gotoTable
	alphaMap := &ac.alphaMap
	stateOut := ac.stateOut

	// Scan text left-to-right using the alphabet-reduced DFA goto table
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])

		// Hot path: alphabet-mapped single array lookup for next state - O(1)!
		state = gotoTable[int(state)*rowWidth+int(alphaMap[b])]

		// Output matches from precomputed state output list
		if outs := stateOut[state]; len(outs) > 0 {
			for _, pi := range outs {
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
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// SearchBytes matches over []byte instead of string. Same semantics as Search.
// Uses precomputed DFA goto table for O(1) transitions.
func (ac *AhoCorasick) SearchBytes(data []byte) []ACMatch {
	if len(ac.patterns) == 0 {
		return nil
	}
	if !ac.built {
		ac.Build()
	}

	result := make([]ACMatch, 0, 8)
	state := int32(0) // Start at root state 0
	rowWidth := int(ac.rowWidth)
	gotoTable := ac.gotoTable
	alphaMap := &ac.alphaMap
	stateOut := ac.stateOut

	for i := 0; i < len(data); i++ {
		b := acLowerByte(data[i])

		// Hot path: alphabet-mapped single array lookup for next state - O(1)!
		state = gotoTable[int(state)*rowWidth+int(alphaMap[b])]

		// Emit matches from the merged output list
		if outs := stateOut[state]; len(outs) > 0 {
			for _, pi := range outs {
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
// Uses precomputed DFA goto table for O(1) transitions.
func (ac *AhoCorasick) SearchInto(text string, v VisitMatches) {
	if len(ac.patterns) == 0 {
		return
	}
	if !ac.built {
		ac.Build()
	}

	state := int32(0) // Start at root state 0
	rowWidth := int(ac.rowWidth)
	gotoTable := ac.gotoTable
	alphaMap := &ac.alphaMap
	stateOut := ac.stateOut
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])

		// Hot path: alphabet-mapped single array lookup for next state - O(1)!
		state = gotoTable[int(state)*rowWidth+int(alphaMap[b])]

		// Emit matches from the merged output list
		if outs := stateOut[state]; len(outs) > 0 {
			for _, pi := range outs {
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
}

// MatchAny returns true if any of the patterns match the input (no allocations).
// Uses precomputed DFA goto table for O(1) transitions.
func (ac *AhoCorasick) MatchAny(text string) bool {
	if len(ac.patterns) == 0 {
		return false
	}
	if !ac.built {
		ac.Build()
	}

	found := false
	state := int32(0) // Start at root state 0
	rowWidth := int(ac.rowWidth)
	gotoTable := ac.gotoTable
	alphaMap := &ac.alphaMap
	stateOut := ac.stateOut
	for i := 0; i < len(text); i++ {
		b := acLowerByte(text[i])

		// Hot path: alphabet-mapped single array lookup for next state - O(1)!
		state = gotoTable[int(state)*rowWidth+int(alphaMap[b])]

		if len(stateOut[state]) > 0 {
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
