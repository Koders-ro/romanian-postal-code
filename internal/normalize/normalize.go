// Package normalize turns free-form Romanian address input into canonical,
// diacritic-free keys that can be compared with the postal dataset.
package normalize

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Fold lowercases s, strips diacritics and replaces every run of
// non-alphanumeric characters with a single space.
//
//	Fold("Str. Ştefan cel Mare, nr.20") == "str stefan cel mare nr 20"
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true // suppress leading space
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			i++
			switch {
			case 'a' <= c && c <= 'z', '0' <= c && c <= '9':
				b.WriteByte(c)
				space = false
			case 'A' <= c && c <= 'Z':
				b.WriteByte(c + 'a' - 'A')
				space = false
			default:
				if !space {
					b.WriteByte(' ')
					space = true
				}
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if rep, ok := diacritics[r]; ok {
			b.WriteString(rep)
			space = false
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSuffix(b.String(), " ")
}

// Key is Fold with all spaces removed: "Târgu-Mureș" -> "targumures".
func Key(s string) string {
	return strings.ReplaceAll(Fold(s), " ", "")
}

// StripParens removes every "(...)" group: "Popa Nan (sector 3)" -> "Popa Nan ".
func StripParens(s string) string {
	if !strings.ContainsRune(s, '(') {
		return s
	}
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Street is a parsed street address.
type Street struct {
	Type   string   `json:"type,omitempty"`   // canonical street type, e.g. "strada", "bulevardul"
	Name   string   `json:"name"`             // folded name without type, titles and number
	Tokens []string `json:"-"`                // words of Name
	Number string   `json:"number,omitempty"` // house number, e.g. "20a"
	Block  string   `json:"block,omitempty"`  // block, e.g. "a2"

	// NumberGuessed is set when Number was a bare trailing number (no "nr."),
	// so it may belong to the street name ("Strada 1 Decembrie 1918").
	NumberGuessed bool `json:"-"`
}

// Key returns the order-independent lookup key of the street name, so
// "Ion Mincu" and the dataset's "Mincu Ion, arh." produce the same key.
func (s Street) Key() string { return BagKey(s.Tokens) }

// LooseKey is Key without single-letter initials ("Brătianu Ion C." -> "bratianuion").
func (s Street) LooseKey() string {
	t := make([]string, 0, len(s.Tokens))
	for _, w := range s.Tokens {
		if len(w) > 1 {
			t = append(t, w)
		}
	}
	return BagKey(t)
}

// WithNumberInName returns the street with a guessed trailing number put back
// into the name.
func (s Street) WithNumberInName() Street {
	out := s
	out.Tokens = append(append([]string(nil), s.Tokens...), splitAlnum(s.Number)...)
	out.Name = strings.Join(out.Tokens, " ")
	out.Number = ""
	out.NumberGuessed = false
	return out
}

// BagKey sorts the tokens and joins them without separators.
func BagKey(tokens []string) string {
	if len(tokens) == 1 {
		return tokens[0]
	}
	t := append([]string(nil), tokens...)
	sort.Strings(t)
	return strings.Join(t, "")
}

// ParseStreet parses inputs such as "Cal. Moșilor 120", "calea mosilor nr. 120",
// "B-dul Ion C. Brătianu nr 5A bl. 3 sc. A ap. 12" or "mosilor".
func ParseStreet(s string) Street {
	tokens := joinBigrams(strings.Fields(Fold(StripParens(s))))
	var st Street

	// Leading street type: "str", "bd", "b dul" (joined above to "bdul")...
	if len(tokens) > 1 {
		if t, ok := streetTypes[tokens[0]]; ok {
			st.Type = t
			tokens = tokens[1:]
		}
	}

	// Split name from number / block / apartment details.
	name := tokens[:0:0]
	for i := 0; i < len(tokens); i++ {
		w := tokens[i]
		if len(name) == 0 {
			// Never treat the first name word as a marker ("Strada Scarii").
			name = append(name, w)
			continue
		}
		switch markers[w] {
		case markerNumber:
			if i+1 < len(tokens) {
				i++
				st.Number = tokens[i]
				if i+1 < len(tokens) && isLetterSuffix(tokens[i+1]) {
					i++
					st.Number += tokens[i]
				}
			}
			continue
		case markerBlock:
			if i+1 < len(tokens) {
				i++
				st.Block = tokens[i]
				// "bl. A 2", "bl. 54/2D" -> "a2", "542d"
				for i+1 < len(tokens) && markers[tokens[i+1]] == 0 &&
					(isLetterSuffix(tokens[i+1]) || isNumber(tokens[i+1])) {
					i++
					st.Block += tokens[i]
				}
			}
			continue
		case markerSkip:
			i++ // drop the marker and its value (sc A, ap 12, et 3)
			continue
		}
		if st.Number != "" || st.Block != "" {
			continue // trailing junk after the number
		}
		name = append(name, w)
	}

	// Bare trailing number: "mosilor 120" / "mosilor 120 a".
	if st.Number == "" && len(name) > 1 {
		n := len(name)
		if n > 2 && isLetterSuffix(name[n-1]) && isNumber(name[n-2]) {
			st.Number, name = name[n-2]+name[n-1], name[:n-2]
			st.NumberGuessed = true
		} else if isNumber(name[n-1]) {
			st.Number, name = name[n-1], name[:n-1]
			st.NumberGuessed = true
		}
	}

	st.Tokens = cleanName(name)
	st.Name = strings.Join(st.Tokens, " ")
	return st
}

// ParseStreetName parses a street name as stored in the dataset
// ("Mincu Ion, arh."), which never contains a number or block.
func ParseStreetName(s string) []string {
	return cleanName(joinBigrams(strings.Fields(Fold(StripParens(s)))))
}

// StreetType maps a street type in any spelling ("Bd.", "Stradă", "Alee") to
// its canonical form, or "" if unknown.
func StreetType(s string) string {
	t := joinBigrams(strings.Fields(Fold(s)))
	if len(t) == 0 {
		return ""
	}
	return streetTypes[t[0]]
}

// cleanName expands abbreviations and drops honorific titles, unless that
// would leave nothing ("Strada Pictor" stays "pictor").
func cleanName(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, w := range tokens {
		if e, ok := expansions[w]; ok {
			w = e
		}
		if !titles[w] {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		for _, w := range tokens {
			if e, ok := expansions[w]; ok {
				w = e
			}
			out = append(out, w)
		}
	}
	return out
}

// joinBigrams merges words split by Fold's punctuation handling: "b dul" -> "bdul".
func joinBigrams(t []string) []string {
	for i := 0; i+1 < len(t); i++ {
		if j, ok := bigrams[t[i]+" "+t[i+1]]; ok {
			t = append(append(t[:i:i], j), t[i+2:]...)
		}
	}
	return t
}

func isNumber(w string) bool {
	if w == "" || w[0] < '0' || w[0] > '9' {
		return false
	}
	// digits followed by at most two letters: "20", "20a", "13bis" is not.
	i := 0
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	return len(w)-i <= 2
}

func isLetterSuffix(w string) bool {
	return len(w) == 1 && w[0] >= 'a' && w[0] <= 'z'
}

func splitAlnum(w string) []string {
	i := 0
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	if i == 0 || i == len(w) {
		return []string{w}
	}
	return []string{w[:i], w[i:]}
}

// Locality normalizes a city/town/village name and extracts a Bucharest
// sector if present. "Mun. Cluj-Napoca" -> ("clujnapoca", 0);
// "Sector 3" / "București S3" -> ("bucuresti", 3).
func Locality(s string) (key string, sector int) {
	tokens := strings.Fields(Fold(StripParens(s)))
	out := tokens[:0:0]
	for i := 0; i < len(tokens); i++ {
		w := tokens[i]
		if (w == "sector" || w == "sectorul" || w == "sect") && i+1 < len(tokens) {
			if n := sectorNum(tokens[i+1]); n > 0 {
				sector = n
				i++
				continue
			}
		}
		if len(w) == 2 && w[0] == 's' {
			if n := sectorNum(w[1:]); n > 0 {
				sector = n
				continue
			}
		}
		if localityPrefixes[w] && len(tokens)-i > 1 && len(out) == 0 {
			continue
		}
		out = append(out, w)
	}
	key = strings.Join(out, "")
	if a, ok := localityAliases[key]; ok {
		key = a
	}
	if key == "" && sector > 0 {
		key = "bucuresti"
	}
	return key, sector
}

// LocalityExact is the key of a locality name as written, without stripping
// "sat"/"comuna"/... prefixes, which are part of names like "Sat Nou".
func LocalityExact(s string) string {
	key := Key(StripParens(s))
	if a, ok := localityAliases[key]; ok {
		return a
	}
	return key
}

// County normalizes a county name or its two-letter code: "jud. Cluj", "CJ",
// "Bistrița Năsăud" -> "cluj", "cluj", "bistritanasaud". The sector, if any,
// is returned for inputs like "Sector 2".
func County(s string) (key string, sector int) {
	key, sector = Locality(s)
	tokens := strings.Fields(Fold(s))
	if len(tokens) > 1 && countyPrefixes[tokens[0]] {
		key, _ = Locality(strings.Join(tokens[1:], " "))
	}
	if c, ok := countyCodes[key]; ok {
		return c, sector
	}
	return key, sector
}

func sectorNum(w string) int {
	if len(w) == 1 && w[0] >= '1' && w[0] <= '6' {
		return int(w[0] - '0')
	}
	return 0
}

// PostalCode extracts a postal code from user input ("RO-400 693" -> "400693").
// ok reports whether the result has the valid 6-digit format.
func PostalCode(s string) (code string, ok bool) {
	var b [8]byte
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			if n == len(b) {
				return strings.TrimSpace(s), false
			}
			b[n] = c
			n++
		} else if c != ' ' && c != '-' && c != '.' && !(n == 0 && (c|0x20 == 'r' || c|0x20 == 'o')) {
			return strings.TrimSpace(s), false
		}
	}
	return string(b[:n]), n == 6
}
