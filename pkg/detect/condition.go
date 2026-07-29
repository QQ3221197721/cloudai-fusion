// Package detect implements a Sigma-compatible detection engine — a real,
// dependency-light evaluator for the industry-standard Sigma rule format
// (https://sigmahq.io). It parses Sigma YAML rules and matches structured log
// events against them, so CloudAI Fusion's operations wells (L3-L7) and hunting
// well (L2) can run thousands of community detection rules instead of a handful
// of hand-coded checks. This is genuine detection depth, not scaffolding.
//
// Supported (a faithful, practical subset of the Sigma spec):
//   - logsource {category, product, service}
//   - detection: named search identifiers (maps, lists of maps, keyword lists)
//   - field modifiers: contains, startswith, endswith, re, all, cidr, base64/…
//     (the high-value common set)
//   - value lists (OR by default, AND with |all)
//   - condition grammar: identifiers, and/or/not, parentheses, and the
//     quantifiers "1|all|any|N of them" and "1|all|N of <prefix>*"
//
// condition.go holds the condition-expression tokenizer, parser, and evaluator.
package detect

import (
	"fmt"
	"strconv"
	"strings"
)

// condExpr is a node in the parsed condition expression tree. It evaluates
// against the set of search-identifiers that matched an event.
type condExpr interface {
	eval(matched map[string]bool, allIDs []string) bool
}

type identExpr struct{ name string }

func (e identExpr) eval(matched map[string]bool, _ []string) bool { return matched[e.name] }

type notExpr struct{ x condExpr }

func (e notExpr) eval(m map[string]bool, ids []string) bool { return !e.x.eval(m, ids) }

type andExpr struct{ l, r condExpr }

func (e andExpr) eval(m map[string]bool, ids []string) bool {
	return e.l.eval(m, ids) && e.r.eval(m, ids)
}

type orExpr struct{ l, r condExpr }

func (e orExpr) eval(m map[string]bool, ids []string) bool {
	return e.l.eval(m, ids) || e.r.eval(m, ids)
}

// quantExpr implements "N of pattern" / "all of pattern" / "1|any of them".
type quantExpr struct {
	all     bool   // "all of" (else threshold-based)
	n       int    // threshold when !all (1 for "1 of"/"any of", N for "N of")
	pattern string // "them", an exact id, or a "prefix*" glob
}

func (e quantExpr) eval(matched map[string]bool, allIDs []string) bool {
	targets := resolvePattern(e.pattern, allIDs)
	if len(targets) == 0 {
		return false
	}
	hit := 0
	for _, id := range targets {
		if matched[id] {
			hit++
		}
	}
	if e.all {
		return hit == len(targets)
	}
	return hit >= e.n
}

// resolvePattern expands a quantifier pattern into concrete search-identifier
// names: "them" = all ids; "prefix*" = ids with that prefix; otherwise exact.
func resolvePattern(pattern string, allIDs []string) []string {
	if pattern == "them" {
		return allIDs
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		out := make([]string, 0, len(allIDs))
		for _, id := range allIDs {
			if strings.HasPrefix(id, prefix) {
				out = append(out, id)
			}
		}
		return out
	}
	return []string{pattern}
}

// ---- tokenizer ----

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tNumber
	tLParen
	tRParen
	tAnd
	tOr
	tNot
	tOf
	tAll
	tAny
	tThem
)

type token struct {
	kind tokKind
	text string
}

// tokenizeCondition splits a Sigma condition into tokens. Identifiers may end
// with '*' (a quantifier glob). Keywords are matched case-insensitively.
func tokenizeCondition(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(':
			toks = append(toks, token{tLParen, "("})
			i++
		case c == ')':
			toks = append(toks, token{tRParen, ")"})
			i++
		case isIdentChar(c):
			j := i
			for j < len(s) && (isIdentChar(s[j]) || s[j] == '*') {
				j++
			}
			word := s[i:j]
			i = j
			toks = append(toks, classifyWord(word))
		default:
			return nil, fmt.Errorf("condition: unexpected character %q", string(c))
		}
	}
	toks = append(toks, token{tEOF, ""})
	return toks, nil
}

func isIdentChar(c byte) bool {
	return c == '_' || c == '-' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func classifyWord(word string) token {
	switch strings.ToLower(word) {
	case "and":
		return token{tAnd, word}
	case "or":
		return token{tOr, word}
	case "not":
		return token{tNot, word}
	case "of":
		return token{tOf, word}
	case "all":
		return token{tAll, word}
	case "any":
		return token{tAny, word}
	case "them":
		return token{tThem, word}
	}
	if _, err := strconv.Atoi(word); err == nil {
		return token{tNumber, word}
	}
	return token{tIdent, word}
}

// ---- parser (recursive descent; precedence: not > and > or) ----

type condParser struct {
	toks []token
	pos  int
}

func parseCondition(s string) (condExpr, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("condition: empty")
	}
	toks, err := tokenizeCondition(s)
	if err != nil {
		return nil, err
	}
	p := &condParser{toks: toks}
	expr, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.cur().kind != tEOF {
		return nil, fmt.Errorf("condition: unexpected token %q", p.cur().text)
	}
	return expr, nil
}

func (p *condParser) cur() token  { return p.toks[p.pos] }
func (p *condParser) next() token { t := p.toks[p.pos]; p.pos++; return t }

func (p *condParser) parseOr() (condExpr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orExpr{left, right}
	}
	return left, nil
}

func (p *condParser) parseAnd() (condExpr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.cur().kind == tAnd {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = andExpr{left, right}
	}
	return left, nil
}

func (p *condParser) parseNot() (condExpr, error) {
	if p.cur().kind == tNot {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return notExpr{x}, nil
	}
	return p.parsePrimary()
}

func (p *condParser) parsePrimary() (condExpr, error) {
	switch t := p.cur(); t.kind {
	case tLParen:
		p.next()
		expr, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.cur().kind != tRParen {
			return nil, fmt.Errorf("condition: expected ')'")
		}
		p.next()
		return expr, nil
	case tAll, tAny, tNumber:
		return p.parseQuantifier()
	case tIdent:
		p.next()
		return identExpr{t.text}, nil
	default:
		return nil, fmt.Errorf("condition: unexpected token %q", t.text)
	}
}

// parseQuantifier parses "all of <pat>", "any of <pat>", "1 of <pat>",
// "<N> of <pat>", where <pat> is "them", an identifier, or a "prefix*" glob.
func (p *condParser) parseQuantifier() (condExpr, error) {
	head := p.next()
	q := quantExpr{}
	switch head.kind {
	case tAll:
		q.all = true
	case tAny:
		q.n = 1
	case tNumber:
		n, _ := strconv.Atoi(head.text)
		if n < 1 {
			n = 1
		}
		q.n = n
	}
	if p.cur().kind != tOf {
		return nil, fmt.Errorf("condition: expected 'of' after quantifier")
	}
	p.next()
	switch t := p.cur(); t.kind {
	case tThem:
		p.next()
		q.pattern = "them"
	case tIdent:
		p.next()
		q.pattern = t.text
	default:
		return nil, fmt.Errorf("condition: expected identifier or 'them' after 'of'")
	}
	return q, nil
}
