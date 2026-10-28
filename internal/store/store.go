// Package store holds the postal dataset in memory, indexed for O(1)
// lookups by code, locality and normalized street name.
package store

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"strings"

	"rocode/internal/normalize"
)

// Entry is one row of the dataset: a postal code and the area it covers.
type Entry struct {
	Code       string `json:"code"`
	County     string `json:"county"`
	Locality   string `json:"locality"`
	Sector     int    `json:"sector,omitempty"`
	StreetType string `json:"street_type,omitempty"`
	Street     string `json:"street,omitempty"`
	Numbers    string `json:"numbers,omitempty"`
	Siruta     string `json:"siruta,omitempty"`
	Label      string `json:"label"`
	LabelASCII string `json:"label_ascii"`

	typeKey   string
	countyKey string
	spec      numberSpec
	loc       *Locality
}

// Locality is a city, town or village. Large cities have street-level codes;
// smaller localities have a single code.
type Locality struct {
	Name   string
	County string
	Siruta string

	key, countyKey string
	entries        []*Entry            // locality-level rows (no street)
	streets        map[string][]*Entry // street key -> rows
	loose          map[string][]*Entry // street key without initials/numerals -> rows
	streetList     []streetRef         // distinct streets, for suggestions
	codes          map[string]struct{}
}

type streetRef struct {
	key   string
	label string
}

// Store is the immutable, concurrency-safe in-memory index.
type Store struct {
	byCode     map[string][]*Entry
	counties   map[string]string // key -> display name
	byCounty   map[string]map[string][]*Locality
	byName     map[string][]*Locality
	prefixes   map[string][]string // first two digits of a code -> counties
	localities []*Locality

	Rows, Codes, Localities, Streets int
}

// Load reads the gzipped CSV produced by tools/importer.
func Load(r io.Reader) (*Store, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	cr := csv.NewReader(gz)
	cr.ReuseRecord = true
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("store: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	for _, h := range []string{"county", "locality", "siruta", "street_type", "street", "number", "code", "sector"} {
		if _, ok := col[h]; !ok {
			return nil, fmt.Errorf("store: missing column %q", h)
		}
	}

	s := &Store{
		byCode:   make(map[string][]*Entry, 40000),
		counties: map[string]string{},
		byCounty: map[string]map[string][]*Locality{},
		byName:   map[string][]*Locality{},
		prefixes: map[string][]string{},
	}
	type locID struct{ county, name, siruta string }
	locs := map[locID]*Locality{}
	strs := map[string]string{} // interning: most values repeat

	intern := func(v string) string {
		v = fixCedilla(strings.TrimSpace(v))
		if i, ok := strs[v]; ok {
			return i
		}
		strs[v] = v
		return v
	}

	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("store: %w", err)
		}
		e := &Entry{
			Code:       rec[col["code"]],
			County:     intern(rec[col["county"]]),
			Locality:   intern(rec[col["locality"]]),
			StreetType: intern(rec[col["street_type"]]),
			Street:     intern(rec[col["street"]]),
			Numbers:    intern(rec[col["number"]]),
			Siruta:     intern(rec[col["siruta"]]),
		}
		if sec := rec[col["sector"]]; len(sec) == 1 {
			e.Sector = int(sec[0] - '0')
		}
		if _, ok := normalize.PostalCode(e.Code); !ok {
			continue
		}
		e.countyKey = normalize.Key(e.County)
		locKey := normalize.LocalityExact(e.Locality)
		siruta := e.Siruta
		if e.Sector > 0 {
			siruta = "" // Bucharest: one locality, sectors are a field
		}
		id := locID{e.countyKey, locKey, siruta}
		loc := locs[id]
		if loc == nil {
			loc = &Locality{
				Name: e.Locality, County: e.County, Siruta: siruta,
				key: locKey, countyKey: e.countyKey,
				codes: map[string]struct{}{},
			}
			if e.Sector > 0 {
				loc.Siruta = "179132"
			}
			locs[id] = loc
			s.localities = append(s.localities, loc)
			if s.byCounty[e.countyKey] == nil {
				s.byCounty[e.countyKey] = map[string][]*Locality{}
			}
			s.byCounty[e.countyKey][locKey] = append(s.byCounty[e.countyKey][locKey], loc)
			s.byName[locKey] = append(s.byName[locKey], loc)
			s.counties[e.countyKey] = e.County
		}
		e.loc = loc
		e.spec = parseSpec(e.Numbers)
		e.Label, e.LabelASCII = label(e)
		loc.codes[e.Code] = struct{}{}

		if e.Street == "" {
			loc.entries = append(loc.entries, e)
		} else {
			s.indexStreet(loc, e)
		}
		s.byCode[e.Code] = append(s.byCode[e.Code], e)
		if p := e.Code[:2]; !slices.Contains(s.prefixes[p], e.County) {
			s.prefixes[p] = append(s.prefixes[p], e.County)
		}
		s.Rows++
	}
	s.Codes = len(s.byCode)
	s.Localities = len(s.localities)
	return s, nil
}

func (s *Store) indexStreet(loc *Locality, e *Entry) {
	if loc.streets == nil {
		loc.streets = map[string][]*Entry{}
		loc.loose = map[string][]*Entry{}
	}
	// "Alee II" + "Rovine" is "Aleea II Rovine": the numeral belongs to the name.
	typeTokens := strings.Fields(normalize.Fold(e.StreetType))
	e.typeKey = normalize.StreetType(e.StreetType)
	name := normalize.ParseStreetName(e.Street)
	full := name
	if len(typeTokens) > 1 {
		full = append(append([]string(nil), typeTokens[1:]...), name...)
	}

	key := normalize.BagKey(full)
	if _, seen := loc.streets[key]; !seen {
		loc.streetList = append(loc.streetList, streetRef{key: key, label: streetLabel(e)})
		s.Streets++
	}
	loc.streets[key] = append(loc.streets[key], e)

	looseKey := normalize.Street{Tokens: name}.LooseKey()
	if looseKey != key {
		loc.loose[looseKey] = append(loc.loose[looseKey], e)
	}
}

// fixCedilla converts the legacy cedilla letters used in the dataset to the
// correct Romanian comma-below letters.
var cedilla = strings.NewReplacer("ş", "ș", "Ş", "Ș", "ţ", "ț", "Ţ", "Ț")

func fixCedilla(s string) string {
	if !strings.ContainsAny(s, "şŞţŢ") {
		return s
	}
	return cedilla.Replace(s)
}

var typeDisplay = map[string]string{
	"strada": "Strada", "bulevardul": "Bulevardul", "calea": "Calea", "aleea": "Aleea",
	"soseaua": "Șoseaua", "piata": "Piața", "piateta": "Piațeta", "splaiul": "Splaiul",
	"intrarea": "Intrarea", "drumul": "Drumul", "fundatura": "Fundătura", "fundacul": "Fundacul",
	"prelungirea": "Prelungirea", "stradela": "Stradela", "pasajul": "Pasajul", "ulita": "Ulița",
	"trecatoarea": "Trecătoarea", "parcul": "Parcul", "colonia": "Colonia", "sirul": "Șirul",
	"scuarul": "Scuarul", "vadul": "Vadul", "podul": "Podul", "hotarul": "Hotarul",
	"campul": "Câmpul", "poteca": "Poteca", "curtea": "Curtea", "cartierul": "Cartierul",
	"platoul": "Platoul", "suisul": "Suișul",
}

func streetLabel(e *Entry) string {
	t := typeDisplay[normalize.StreetType(e.StreetType)]
	if t == "" {
		t = e.StreetType
	} else if f := strings.Fields(e.StreetType); len(f) > 1 {
		t += " " + strings.Join(f[1:], " ") // "Aleea II"
	}
	return strings.TrimSpace(t + " " + e.Street)
}

func label(e *Entry) (string, string) {
	var b strings.Builder
	if e.Street != "" {
		b.WriteString(streetLabel(e))
		if e.Numbers != "" {
			b.WriteString(" ")
			b.WriteString(e.Numbers)
		}
		b.WriteString(", ")
	}
	b.WriteString(e.Locality)
	if e.Sector > 0 {
		fmt.Fprintf(&b, ", Sector %d", e.Sector)
	} else {
		b.WriteString(", jud. ")
		b.WriteString(e.County)
	}
	b.WriteString(", ")
	b.WriteString(e.Code)
	l := b.String()
	return l, asciiLabel(l)
}

// asciiLabel strips diacritics but keeps case and punctuation.
func asciiLabel(s string) string {
	return asciiReplacer.Replace(s)
}

var asciiReplacer = strings.NewReplacer(
	"ă", "a", "Ă", "A", "â", "a", "Â", "A", "î", "i", "Î", "I",
	"ș", "s", "Ș", "S", "ş", "s", "Ş", "S", "ț", "t", "Ț", "T", "ţ", "t", "Ţ", "T",
	"é", "e", "ö", "o", "ü", "u", "á", "a", "ő", "o",
)

// CountyName returns the display name of a county key, or "".
func (s *Store) CountyName(key string) string { return s.counties[key] }

// codesOf returns the distinct codes of entries, in order of appearance.
func codesOf(es []*Entry) []string {
	var out []string
	for _, e := range es {
		if !slices.Contains(out, e.Code) {
			out = append(out, e.Code)
		}
	}
	return out
}
