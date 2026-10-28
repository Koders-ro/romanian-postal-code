package store

import (
	"sort"
	"strings"

	"rocode/internal/normalize"
)

const maxSuggestions = 5

type scored struct {
	label string
	score int
}

// best keeps the top-scoring labels (lower score is better).
func best(cands []scored) []string {
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	var out []string
	for _, c := range cands {
		if len(out) == maxSuggestions {
			break
		}
		dup := false
		for _, o := range out {
			dup = dup || o == c.label
		}
		if !dup {
			out = append(out, c.label)
		}
	}
	return out
}

// similarity returns an edit-distance-like score, or -1 if a and b are too
// different to be worth suggesting.
func similarity(input, cand string) int {
	if len(input) >= 4 && strings.Contains(cand, input) {
		return 1 + (len(cand)-len(input))/8
	}
	limit := len(input) / 3
	if limit < 2 {
		limit = 2
	}
	d := levenshtein(input, cand, limit)
	if d > limit {
		return -1
	}
	return d * 2
}

// levenshtein computes the edit distance, giving up once it exceeds max.
func levenshtein(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func (s *Store) suggestCounties(key string) []string {
	var c []scored
	for k, name := range s.counties {
		if sc := similarity(key, k); sc >= 0 {
			c = append(c, scored{name, sc})
		}
	}
	return best(c)
}

func (s *Store) suggestLocalities(key, countyKey string) []string {
	var c []scored
	consider := func(l *Locality) {
		if sc := similarity(key, l.key); sc >= 0 {
			c = append(c, scored{l.Name + ", jud. " + l.County, sc})
		}
	}
	if countyKey != "" {
		for _, ls := range s.byCounty[countyKey] {
			for _, l := range ls {
				consider(l)
			}
		}
	} else {
		for _, l := range s.localities {
			consider(l)
		}
	}
	return best(c)
}

func (l *Locality) suggestStreets(st normalize.Street) []string {
	key := st.Key()
	if key == "" {
		return nil
	}
	var c []scored
	for _, ref := range l.streetList {
		if sc := similarity(key, ref.key); sc >= 0 {
			c = append(c, scored{ref.label + ", " + l.Name, sc})
		}
	}
	return best(c)
}
