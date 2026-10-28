// Package api exposes the store over HTTP/JSON.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"rocode/internal/normalize"
	"rocode/internal/store"
)

// maxField caps every input string. Real addresses are far shorter, and the
// cap bounds the cost of fuzzy suggestions.
const maxField = 200

// Config configures the HTTP handler.
type Config struct {
	APIKeys    []string // any of these keys is accepted
	MaxBatch   int      // max items per batch request
	RateLimit  int      // requests per minute per client IP; 0 disables
	TrustProxy bool     // take the client IP from X-Forwarded-For / X-Real-IP
	Version    string
}

type server struct {
	store *store.Store
	cfg   Config
}

// New returns the root handler.
func New(s *store.Store, cfg Config) http.Handler {
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 500
	}
	srv := &server{store: s, cfg: cfg}

	api := http.NewServeMux()
	api.HandleFunc("GET /v1/codes/{code}", srv.code)
	api.HandleFunc("GET /v1/verify", srv.verify)
	api.HandleFunc("POST /v1/verify", srv.verify)
	api.HandleFunc("POST /v1/verify/batch", srv.batch)
	api.HandleFunc("GET /v1/lookup", srv.lookup)
	api.HandleFunc("GET /v1/normalize", srv.normalize)
	api.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", srv.health)
	mux.HandleFunc("GET /{$}", srv.index)
	mux.Handle("/v1/", srv.rateLimit(srv.auth(api)))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return recoverer(srv.logger(securityHeaders(gzipper(mux))))
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "Romanian postal code API",
		"version": s.cfg.Version,
		"auth":    "send the API key as X-API-Key header, Authorization: Bearer <key> or ?api_key=",
		"endpoints": []string{
			"GET  /v1/codes/{code}",
			"GET  /v1/verify?code=&county=&city=&street=&number=",
			"POST /v1/verify          {code, county, city, street, number}",
			"POST /v1/verify/batch    {items: [{...}, ...]}",
			"GET  /v1/lookup?county=&city=&street=&number=",
			"GET  /v1/normalize?street=&city=&county=&code=",
			"GET  /healthz",
		},
		"data": "Poșta Română Infocod (data.gov.ro, OGL-ROU-1.0)",
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "version": s.cfg.Version, "rows": s.store.Rows, "codes": s.store.Codes,
		"localities": s.store.Localities, "streets": s.store.Streets,
	})
}

func (s *server) code(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if len(code) > maxField {
		writeError(w, http.StatusBadRequest, "code too long")
		return
	}
	info := s.store.Decode(code)
	status := http.StatusOK
	if !info.FormatValid {
		status = http.StatusBadRequest
	} else if !info.Exists {
		status = http.StatusNotFound
	}
	writeJSON(w, status, info)
}

func (s *server) verify(w http.ResponseWriter, r *http.Request) {
	q, ok := readQuery(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.store.Verify(q))
}

func (s *server) batch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []store.Query `json:"items"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if len(body.Items) > s.cfg.MaxBatch {
		writeError(w, http.StatusRequestEntityTooLarge, "too many items; the maximum is "+strconv.Itoa(s.cfg.MaxBatch))
		return
	}
	for i, q := range body.Items {
		if !validQuery(q) {
			writeError(w, http.StatusBadRequest, "item "+strconv.Itoa(i)+": a field exceeds "+strconv.Itoa(maxField)+" characters")
			return
		}
	}
	results := make([]store.Result, len(body.Items))
	for i, q := range body.Items {
		results[i] = s.store.Verify(q)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *server) lookup(w http.ResponseWriter, r *http.Request) {
	q, ok := readQuery(w, r)
	if !ok {
		return
	}
	q.Code = ""
	if q.City == "" && q.County == "" {
		writeError(w, http.StatusBadRequest, "city (or county) is required")
		return
	}
	res := s.store.Verify(q)
	writeJSON(w, http.StatusOK, struct {
		Found       bool               `json:"found"`
		Codes       []string           `json:"codes"`
		Matches     []*store.Entry     `json:"matches,omitempty"`
		Normalized  store.Normalized   `json:"normalized"`
		Checks      store.Checks       `json:"checks"`
		Suggestions *store.Suggestions `json:"suggestions,omitempty"`
		Messages    []string           `json:"messages,omitempty"`
	}{
		len(res.ExpectedCodes) > 0, res.ExpectedCodes, res.Matches,
		res.Normalized, res.Checks, res.Suggestions, res.Messages,
	})
}

func (s *server) normalize(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	for _, k := range []string{"street", "city", "county", "code"} {
		if len(v.Get(k)) > maxField {
			writeError(w, http.StatusBadRequest, k+" exceeds "+strconv.Itoa(maxField)+" characters")
			return
		}
	}
	out := map[string]any{}
	if x := v.Get("street"); x != "" {
		st := normalize.ParseStreet(x)
		out["street"] = map[string]any{
			"input": x, "folded": normalize.Fold(x), "type": st.Type, "name": st.Name,
			"key": st.Key(), "number": st.Number, "block": st.Block,
		}
	}
	if x := v.Get("city"); x != "" {
		key, sector := normalize.Locality(x)
		out["city"] = map[string]any{"input": x, "folded": normalize.Fold(x), "key": key, "sector": sector}
	}
	if x := v.Get("county"); x != "" {
		key, _ := normalize.County(x)
		out["county"] = map[string]any{"input": x, "key": key, "name": s.store.CountyName(key)}
	}
	if x := v.Get("code"); x != "" {
		code, ok := normalize.PostalCode(x)
		out["code"] = map[string]any{"input": x, "code": code, "format_valid": ok}
	}
	if len(out) == 0 {
		writeError(w, http.StatusBadRequest, "pass at least one of: street, city, county, code")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// readQuery reads a Query from URL parameters (GET) or a JSON body (POST)
// and writes a 400 response if it is invalid.
func readQuery(w http.ResponseWriter, r *http.Request) (store.Query, bool) {
	var q store.Query
	if r.Method == http.MethodPost {
		if !decodeBody(w, r, &q) {
			return q, false
		}
	} else {
		v := r.URL.Query()
		q = store.Query{
			Code: v.Get("code"), County: v.Get("county"), City: v.Get("city"),
			Street: v.Get("street"), Number: v.Get("number"),
		}
		if q.City == "" {
			q.City = v.Get("locality")
		}
	}
	if !validQuery(q) {
		writeError(w, http.StatusBadRequest, "a field exceeds "+strconv.Itoa(maxField)+" characters")
		return q, false
	}
	return q, true
}

func validQuery(q store.Query) bool {
	return len(q.Code) <= maxField && len(q.County) <= maxField && len(q.City) <= maxField &&
		len(q.Street) <= maxField && len(q.Number) <= maxField
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
