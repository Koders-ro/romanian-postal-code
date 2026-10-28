package store

import (
	"strings"

	"rocode/internal/normalize"
)

// The dataset describes which part of a street a code covers with specs like
// "nr. 1-25", "nr. 21-T" (odd numbers from 21 to the end),
// "nr. 1-13A; 2-24", "bl. A1-A3, B7" or "nr. 2-T (case)".

const openEnd = 1 << 30

// addr is a house number or block designator split into parts: "A12B" -> {a 12 b}.
type addr struct {
	pre string
	num int
	suf string
}

func parseAddr(s string) addr {
	s = strings.ReplaceAll(normalize.Fold(s), " ", "")
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	a := addr{pre: s[:i], num: -1}
	if i == len(s) {
		return a
	}
	j := i
	n := 0
	for j < len(s) && s[j] >= '0' && s[j] <= '9' && n < openEnd/10 {
		n = n*10 + int(s[j]-'0')
		j++
	}
	a.num, a.suf = n, s[j:]
	return a
}

type spanKind byte

const (
	kindNumber spanKind = 'n'
	kindBlock  spanKind = 'b'
)

// span is one item of a spec: a single number/block or a range.
type span struct {
	lo, hi addr
	single bool
}

// numberSpec is a parsed "Numar" column. An empty spec covers the whole street.
type numberSpec struct {
	kind  spanKind
	spans []span
}

func parseSpec(raw string) numberSpec {
	s := strings.ToLower(strings.TrimSpace(normalize.StripParens(raw)))
	if s == "" {
		return numberSpec{}
	}
	spec := numberSpec{kind: kindNumber}
	switch {
	case strings.HasPrefix(s, "nr."):
		s = s[3:]
	case strings.HasPrefix(s, "bl."):
		spec.kind = kindBlock
		s = s[3:]
	}
	// "bl. B9 sc. A, B, C" - staircases are not addressable here.
	if i := strings.Index(s, " sc."); i >= 0 {
		s = s[:i]
	}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		if !isRange {
			a := parseAddr(part)
			spec.spans = append(spec.spans, span{lo: a, hi: a, single: true})
			continue
		}
		sp := span{lo: parseAddr(lo)}
		if h := strings.TrimSpace(hi); h == "t" || h == "T" {
			sp.hi = addr{pre: sp.lo.pre, num: openEnd}
		} else {
			sp.hi = parseAddr(h)
		}
		spec.spans = append(spec.spans, sp)
	}
	return spec
}

// matchNumber reports whether house number n ("20", "13a") falls in the spec.
// Ranges keep the parity of their start: "1-25" covers odd numbers only.
func (s numberSpec) matchNumber(n addr) bool {
	if s.kind != kindNumber || n.num < 0 {
		return false
	}
	for _, sp := range s.spans {
		if sp.single {
			if sp.lo.num == n.num && (sp.lo.suf == "" || sp.lo.suf == n.suf) {
				return true
			}
			continue
		}
		if sp.lo.num < 0 || sp.hi.num < 0 {
			continue
		}
		if n.num >= sp.lo.num && n.num <= sp.hi.num && n.num%2 == sp.lo.num%2 {
			// "1-13A": 13B is beyond the range end.
			if n.num == sp.hi.num && sp.hi.suf != "" && n.suf > sp.hi.suf {
				continue
			}
			if n.num == sp.lo.num && sp.lo.suf != "" && n.suf < sp.lo.suf {
				continue
			}
			return true
		}
	}
	return false
}

// matchBlock reports whether block b ("a2", "51", "32c") is covered by the spec.
func (s numberSpec) matchBlock(b addr) bool {
	if s.kind != kindBlock {
		return false
	}
	for _, sp := range s.spans {
		switch {
		case sp.single:
			if sp.lo == b {
				return true
			}
		case sp.lo.pre != b.pre:
		case sp.lo.num == sp.hi.num: // "32B-32D"
			if b.num == sp.lo.num && b.suf >= sp.lo.suf && b.suf <= sp.hi.suf {
				return true
			}
		case b.num >= sp.lo.num && b.num <= sp.hi.num && b.suf == sp.lo.suf: // "A1-A3", "512-517"
			return true
		}
	}
	return false
}
