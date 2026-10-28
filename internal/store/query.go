package store

import (
	"fmt"
	"slices"
	"strings"

	"rocode/internal/normalize"
)

// CodeInfo is the decoded information about a postal code.
type CodeInfo struct {
	Input       string   `json:"input"`
	Code        string   `json:"code"`
	FormatValid bool     `json:"format_valid"`
	Exists      bool     `json:"exists"`
	Region      *int     `json:"region,omitempty"`
	County      string   `json:"county,omitempty"`
	Locality    string   `json:"locality,omitempty"`
	Sector      int      `json:"sector,omitempty"`
	Level       string   `json:"level,omitempty"` // locality | street | street_segment
	Entries     []*Entry `json:"entries,omitempty"`
	Probable    []string `json:"probable_counties,omitempty"`
}

// Decode validates a postal code and returns everything known about it.
func (s *Store) Decode(input string) CodeInfo {
	code, ok := normalize.PostalCode(input)
	info := CodeInfo{Input: input, Code: code, FormatValid: ok}
	if !ok {
		return info
	}
	region := int(code[0] - '0')
	info.Region = &region
	es := s.byCode[code]
	if len(es) == 0 {
		info.Probable = s.prefixes[code[:2]]
		return info
	}
	info.Exists = true
	info.Entries = es
	info.County, info.Locality, info.Sector = es[0].County, es[0].Locality, es[0].Sector
	info.Level = "locality"
	for _, e := range es {
		if e.Locality != info.Locality {
			info.Locality = ""
		}
		if e.County != info.County {
			info.County = ""
		}
		if e.Street != "" && info.Level == "locality" {
			info.Level = "street"
		}
		if e.Numbers != "" {
			info.Level = "street_segment"
		}
	}
	return info
}

// Query is an address to verify or look up. All fields are optional.
type Query struct {
	Code   string `json:"code,omitempty"`
	County string `json:"county,omitempty"`
	City   string `json:"city,omitempty"`
	Street string `json:"street,omitempty"`
	Number string `json:"number,omitempty"`
}

// Checks holds the outcome of each check. A nil check was not performed,
// because the input was missing or the data has no street-level detail.
type Checks struct {
	CodeFormat         *bool `json:"code_format,omitempty"`
	CodeExists         *bool `json:"code_exists,omitempty"`
	County             *bool `json:"county,omitempty"`
	City               *bool `json:"city,omitempty"`
	Street             *bool `json:"street,omitempty"`
	Number             *bool `json:"number,omitempty"`
	CodeMatchesAddress *bool `json:"code_matches_address,omitempty"`
}

// Normalized is the canonical form of the query.
type Normalized struct {
	Code   string            `json:"code,omitempty"`
	County string            `json:"county,omitempty"`
	City   string            `json:"city,omitempty"`
	Sector int               `json:"sector,omitempty"`
	Street *normalize.Street `json:"street,omitempty"`
}

// Suggestions are close matches for inputs that were not found.
type Suggestions struct {
	County []string `json:"county,omitempty"`
	City   []string `json:"city,omitempty"`
	Street []string `json:"street,omitempty"`
}

// Result of Verify.
type Result struct {
	Valid         bool         `json:"valid"`
	Normalized    Normalized   `json:"normalized"`
	Checks        Checks       `json:"checks"`
	ExpectedCodes []string     `json:"expected_codes,omitempty"`
	Matches       []*Entry     `json:"matches,omitempty"`
	Suggestions   *Suggestions `json:"suggestions,omitempty"`
	Messages      []string     `json:"messages,omitempty"`
}

func ptr(b bool) *bool { return &b }

func (r *Result) suggest() *Suggestions {
	if r.Suggestions == nil {
		r.Suggestions = &Suggestions{}
	}
	return r.Suggestions
}

func (r *Result) msg(format string, args ...any) {
	r.Messages = append(r.Messages, fmt.Sprintf(format, args...))
}

// Verify checks that the given parts of an address are consistent with each
// other and with the dataset, and returns the postal code(s) the address
// should have.
func (s *Store) Verify(q Query) Result {
	var r Result

	// Postal code.
	var codeEntries []*Entry
	if strings.TrimSpace(q.Code) != "" {
		code, ok := normalize.PostalCode(q.Code)
		r.Normalized.Code = code
		r.Checks.CodeFormat = ptr(ok)
		if ok {
			codeEntries = s.byCode[code]
			r.Checks.CodeExists = ptr(len(codeEntries) > 0)
			if len(codeEntries) == 0 {
				r.msg("postal code %s does not exist", code)
			}
		} else {
			r.msg("postal code must have exactly 6 digits")
		}
	}

	// County.
	var countyKey string
	sector := 0
	if strings.TrimSpace(q.County) != "" {
		countyKey, sector = normalize.County(q.County)
		r.Normalized.County = countyKey
		if _, ok := s.counties[countyKey]; ok {
			r.Checks.County = ptr(true)
		} else {
			r.Checks.County = ptr(false)
			r.suggest().County = s.suggestCounties(countyKey)
			r.msg("unknown county %q", q.County)
			countyKey = ""
		}
	}

	// City.
	var locs []*Locality
	if strings.TrimSpace(q.City) != "" {
		key, sec := normalize.Locality(q.City)
		if sec > 0 {
			sector = sec
		}
		// "Sat Nou" is a name, "Mun. Cluj-Napoca" has a prefix: try both.
		if exact := normalize.LocalityExact(q.City); sec == 0 && exact != key && len(s.byName[exact]) > 0 {
			key = exact
		}
		r.Normalized.City = key
		if countyKey != "" {
			locs = s.byCounty[countyKey][key]
		} else {
			locs = s.byName[key]
		}
		switch {
		case len(locs) > 0:
			r.Checks.City = ptr(true)
		case countyKey != "" && len(s.byName[key]) > 0:
			locs = s.byName[key]
			r.Checks.City = ptr(true)
			r.Checks.County = ptr(false)
			r.msg("%s is not in county %s (found in: %s)", locs[0].Name, s.counties[countyKey], strings.Join(countiesOf(locs), ", "))
		default:
			r.Checks.City = ptr(false)
			r.suggest().City = s.suggestLocalities(key, countyKey)
			r.msg("unknown locality %q", q.City)
		}
	} else if len(codeEntries) > 0 {
		for _, e := range codeEntries {
			if !slices.Contains(locs, e.loc) {
				locs = append(locs, e.loc)
			}
		}
	}
	r.Normalized.Sector = sector

	// Street and number.
	var matches []*Entry
	if strings.TrimSpace(q.Street) != "" {
		st := normalize.ParseStreet(q.Street)
		if n := strings.TrimSpace(q.Number); n != "" {
			if p := normalize.ParseStreet("x nr " + n); p.Number != "" {
				st.Number, st.NumberGuessed = p.Number, false
			}
			if p := normalize.ParseStreet("x " + n); p.Block != "" {
				st.Number, st.Block = "", p.Block
			}
		}
		matches = s.verifyStreet(&r, &st, locs, sector)
		r.Normalized.Street = &st
	} else if len(locs) > 0 {
		for _, l := range locs {
			if l.streets == nil {
				matches = append(matches, l.entries...)
			}
		}
	}
	if len(locs) == 0 && strings.TrimSpace(q.Street) != "" && r.Checks.City == nil {
		r.msg("a city or a valid postal code is needed to verify the street")
	}

	r.Matches = matches
	r.ExpectedCodes = codesOf(matches)

	// Does the given code fit the address?
	if len(codeEntries) > 0 {
		code := r.Normalized.Code
		var ok, decided bool
		switch {
		case len(matches) > 0:
			ok, decided = slices.Contains(r.ExpectedCodes, code), true
		case len(locs) > 0 && q.City != "":
			ok, decided = false, true
			for _, l := range locs {
				if _, in := l.codes[code]; in {
					ok = true
				}
			}
		case countyKey != "":
			ok, decided = false, true
			for _, e := range codeEntries {
				ok = ok || e.countyKey == countyKey
			}
		}
		if decided {
			r.Checks.CodeMatchesAddress = ptr(ok)
			if !ok {
				r.msg("postal code %s does not match the address", code)
			}
		}
	}

	r.Valid = r.allPassed()
	return r
}

func (s *Store) verifyStreet(r *Result, st *normalize.Street, locs []*Locality, sector int) []*Entry {
	if len(locs) == 0 {
		return nil
	}
	var matches []*Entry
	streetLevel := false
	for _, l := range locs {
		if l.streets == nil {
			continue
		}
		streetLevel = true
		es, used := l.findStreet(*st)
		if len(es) > 0 {
			*st = used
			matches = append(matches, es...)
		}
	}
	if !streetLevel {
		var es []*Entry
		for _, l := range locs {
			es = append(es, l.entries...)
		}
		r.msg("%s has a single postal code; street-level data is not available", locs[0].Name)
		return es
	}
	if len(matches) == 0 {
		r.Checks.Street = ptr(false)
		for _, l := range locs {
			r.suggest().Street = append(r.suggest().Street, l.suggestStreets(*st)...)
		}
		r.msg("street %q not found", st.Name)
		return nil
	}
	r.Checks.Street = ptr(true)

	// Narrow down by sector and street type when that is unambiguous.
	if sector > 0 {
		if f := filter(matches, func(e *Entry) bool { return e.Sector == sector }); len(f) > 0 {
			matches = f
		} else {
			r.msg("street not found in sector %d", sector)
		}
	}
	if st.Type != "" {
		if f := filter(matches, func(e *Entry) bool { return e.typeKey == st.Type }); len(f) > 0 {
			matches = f
		}
	}

	if st.Number == "" && st.Block == "" {
		return matches
	}
	var hit []*Entry
	if st.Block != "" {
		b := parseAddr(st.Block)
		hit = filter(matches, func(e *Entry) bool { return e.spec.matchBlock(b) })
	}
	if len(hit) == 0 && st.Number != "" {
		n := parseAddr(st.Number)
		hit = filter(matches, func(e *Entry) bool { return e.spec.matchNumber(n) })
	}
	if len(hit) == 0 {
		hit = filter(matches, func(e *Entry) bool { return e.Numbers == "" })
	}
	if len(hit) == 0 {
		r.Checks.Number = ptr(false)
		r.msg("number not found in the ranges of this street; showing all its codes")
		return matches
	}
	r.Checks.Number = ptr(true)
	return hit
}

// findStreet looks a parsed street up, also trying it with a guessed number
// put back into the name ("Strada 1 Decembrie 1918").
func (l *Locality) findStreet(st normalize.Street) ([]*Entry, normalize.Street) {
	try := func(s normalize.Street) []*Entry {
		if es := l.streets[s.Key()]; es != nil {
			return es
		}
		return l.loose[s.LooseKey()]
	}
	if st.NumberGuessed {
		alt := st.WithNumberInName()
		if es := try(alt); es != nil {
			return es, alt
		}
	}
	return try(st), st
}

func (r *Result) allPassed() bool {
	c := r.Checks
	any := false
	for _, b := range []*bool{c.CodeFormat, c.CodeExists, c.County, c.City, c.Street, c.Number, c.CodeMatchesAddress} {
		if b != nil {
			if !*b {
				return false
			}
			any = true
		}
	}
	return any
}

func filter(es []*Entry, keep func(*Entry) bool) []*Entry {
	var out []*Entry
	for _, e := range es {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

func countiesOf(locs []*Locality) []string {
	var out []string
	for _, l := range locs {
		if !slices.Contains(out, l.County) {
			out = append(out, l.County)
		}
	}
	return out
}
