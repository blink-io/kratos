package http

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/go-kratos/kratos/v3/transport"
)

// routeKind classifies registered routes.
type routeKind int

const (
	// routeKindNormal is a regular path route registered natively on chi.
	routeKindNormal routeKind = iota
	// routeKindPrefix is a path-prefix route (mux PathPrefix equivalent).
	routeKindPrefix
	// routeKindFallback is a route whose pattern cannot be expressed in chi
	// (e.g. a multi-segment regexp in the middle of the pattern) and is
	// matched by a full-path regexp dispatcher instead.
	routeKindFallback
	// routeKindHeader is a header-matching route (mux Headers equivalent).
	routeKindHeader
)

// reconSeg is one segment of a reconstructed path variable:
// either a literal string or a reference to a chi URL param.
type reconSeg struct {
	literal string
	param   string // chi param key ("*" for the catch-all param)
}

// varRecon describes how to rebuild a mux-style path variable value from
// chi URL params, e.g. {name=publishers/*\/books/*} is registered on chi as
// /publishers/{name__0}/books/{name__1} and rebuilt as "publishers/x/books/y".
type varRecon struct {
	name string
	segs []reconSeg
}

// routeEntry is a single registered route kept in registration order,
// mirroring gorilla/mux route semantics.
type routeEntry struct {
	method   string // empty means all methods ("*")
	pattern  string // original mux-style pattern as registered
	chiPat   string // translated chi pattern
	kind     routeKind
	matchRE  *regexp.Regexp // mux-style full-path matcher (fallback/strict-slash/405)
	varNames []string       // capture group names of matchRE (fallback routes)
	recon    []varRecon     // variable reconstructions for chi-native routes
	header   [2]string      // key/value for routeKindHeader
	handler  http.Handler   // endpoint for fallback/header dispatchers
}

// routeRegistry keeps all registered routes in registration order.
type routeRegistry struct {
	mu sync.RWMutex

	entries  []*routeEntry
	byChiPat map[string]string // chi pattern -> original pattern ("" for prefix routes)

	fallbackGroups map[string]*fallbackGroup // chi wildcard pattern -> group
	headerRoutes   []*routeEntry
	headerMounted  bool
}

// fallbackGroup dispatches requests that hit a chi wildcard pattern to the
// first fallback entry whose full-path regexp matches (registration order).
type fallbackGroup struct {
	entries []*routeEntry
}

func newRouteRegistry() *routeRegistry {
	return &routeRegistry{
		byChiPat:       make(map[string]string),
		fallbackGroups: make(map[string]*fallbackGroup),
	}
}

func (r *routeRegistry) add(e *routeEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	switch e.kind {
	case routeKindNormal:
		r.byChiPat[e.chiPat] = e.pattern
	case routeKindPrefix:
		// mux GetPathTemplate fails for prefix routes, so the path
		// template degrades to "".
		r.byChiPat[e.chiPat] = ""
	case routeKindHeader:
		r.headerRoutes = append(r.headerRoutes, e)
	}
}

// addFallback registers a fallback entry and returns its dispatcher group
// plus whether the group was newly created (i.e. the chi wildcard route still
// needs to be registered).
func (r *routeRegistry) addFallback(e *routeEntry) (*fallbackGroup, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	g := r.fallbackGroups[e.chiPat]
	created := g == nil
	if created {
		g = &fallbackGroup{}
		r.fallbackGroups[e.chiPat] = g
	}
	g.entries = append(g.entries, e)
	return g, created
}

// original returns the original mux-style pattern for a chi pattern.
func (r *routeRegistry) original(chiPat string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byChiPat[chiPat]
	return p, ok
}

// headers returns the registered header routes.
func (r *routeRegistry) headers() []*routeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*routeEntry(nil), r.headerRoutes...)
}

func (r *routeRegistry) snapshot() []*routeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*routeEntry(nil), r.entries...)
}

// methodNotAllowed reports whether path matches some route for a different
// method, mirroring mux's 405 semantics.
func (r *routeRegistry) methodNotAllowed(method, path string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.kind == routeKindHeader || e.method == "" || e.method == method {
			continue
		}
		if entryPathMatch(e, path) {
			return true
		}
	}
	return false
}

// patternVar is one {name} or {name:regex} variable found in a pattern.
type patternVar struct {
	name       string
	regex      string
	start, end int
}

// scanPatternVars extracts mux-style variables from a pattern, tolerating
// balanced braces inside regexes (e.g. {id:[0-9]{1,2}}).
func scanPatternVars(pattern string) []patternVar {
	var vars []patternVar
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '{' {
			continue
		}
		depth := 0
		j := i
		for ; j < len(pattern); j++ {
			switch pattern[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					goto closed
				}
			}
		}
	closed:
		if j >= len(pattern) {
			break // unbalanced, treat the rest as literal
		}
		spec := pattern[i+1 : j]
		name, rex := spec, ""
		if idx := strings.IndexByte(spec, ':'); idx >= 0 {
			name, rex = spec[:idx], spec[idx+1:]
		}
		vars = append(vars, patternVar{name: name, regex: rex, start: i, end: j + 1})
		i = j
	}
	return vars
}

// splitRegexSegs splits a regexp on '/' characters that are outside of
// character classes, e.g. `publishers/[^/]+` -> ["publishers", "[^/]+"].
func splitRegexSegs(re string) []string {
	var (
		segs    []string
		cur     strings.Builder
		inClass bool
	)
	for i := 0; i < len(re); i++ {
		c := re[i]
		switch {
		case c == '\\' && i+1 < len(re):
			cur.WriteByte(c)
			cur.WriteByte(re[i+1])
			i++
		case c == '[':
			inClass = true
			cur.WriteByte(c)
		case c == ']':
			inClass = false
			cur.WriteByte(c)
		case c == '/' && !inClass:
			segs = append(segs, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(segs, cur.String())
}

// regexLiteral returns the literal string matched by re when re matches
// exactly one string.
func regexLiteral(re string) (string, bool) {
	compiled, err := regexp.Compile(re)
	if err != nil {
		return "", false
	}
	lit, complete := compiled.LiteralPrefix()
	return lit, complete
}

// staticPrefix returns the literal prefix of pattern before the first variable.
func staticPrefix(pattern string) string {
	if i := strings.IndexByte(pattern, '{'); i >= 0 {
		return pattern[:i]
	}
	return pattern
}

// buildMatcher compiles a mux-style full-path regexp for pattern and returns
// the variable names in capture-group order.
func buildMatcher(pattern string) (*regexp.Regexp, []string) {
	vars := scanPatternVars(pattern)
	var b strings.Builder
	b.WriteByte('^')
	last := 0
	names := make([]string, 0, len(vars))
	for _, v := range vars {
		b.WriteString(regexp.QuoteMeta(pattern[last:v.start]))
		re := v.regex
		if re == "" {
			re = "[^/]+"
		}
		b.WriteByte('(')
		b.WriteString(re)
		b.WriteByte(')')
		names = append(names, v.name)
		last = v.end
	}
	b.WriteString(regexp.QuoteMeta(pattern[last:]))
	b.WriteByte('$')
	return regexp.MustCompile(b.String()), names
}

// translatePattern converts a mux-style pattern into a chi pattern.
//
// Rules:
//   - {name} and single-segment {name:regex} are chi-native and kept as-is.
//   - Multi-segment regexps (e.g. generated from {name=publishers/*\/books/*})
//     are split per segment: literal segments are inlined, variable segments
//     become synthetic params {name__N}, and the original value is rebuilt by
//     a varRecon.
//   - A trailing .* (from {name=**} or {name:.*}) at the end of the pattern
//     becomes a chi catch-all '*'.
//   - Anything else (e.g. a multi-segment regexp in the middle) cannot be
//     expressed in chi and is matched by a fallback full-path regexp.
func translatePattern(e *routeEntry) {
	pattern := e.pattern
	vars := scanPatternVars(pattern)
	var b strings.Builder
	last := 0
	for _, v := range vars {
		b.WriteString(pattern[last:v.start])
		segs := splitRegexSegs(v.regex)
		switch {
		case v.regex == "":
			b.WriteString(pattern[v.start:v.end])
		case len(segs) == 1 && !strings.Contains(v.regex, ".*"):
			// single-segment regexp, chi-compatible
			b.WriteString(pattern[v.start:v.end])
		default:
			rc, chiVar, ok := translateVar(v, segs, pattern[v.end:])
			if !ok {
				// not expressible in chi: fall back to full-path regexp matching
				e.kind = routeKindFallback
				e.matchRE, e.varNames = buildMatcher(pattern)
				e.chiPat = fallbackPattern(staticPrefix(pattern))
				return
			}
			b.WriteString(chiVar)
			e.recon = append(e.recon, rc)
		}
		last = v.end
	}
	b.WriteString(pattern[last:])
	e.chiPat = b.String()
	e.matchRE, _ = buildMatcher(pattern)
}

// translateVar translates one multi-segment variable into chi syntax.
// rest is the pattern text following the variable, used to decide whether the
// variable is the trailing element of the pattern.
func translateVar(v patternVar, segs []string, rest string) (varRecon, string, bool) {
	rc := varRecon{name: v.name}
	var b strings.Builder
	syn := 0
	for i, seg := range segs {
		if i > 0 {
			b.WriteByte('/')
		}
		switch {
		case strings.Contains(seg, ".*"):
			// multi-segment wildcard: only allowed as the trailing chi catch-all
			if i != len(segs)-1 || rest != "" {
				return rc, "", false
			}
			b.WriteByte('*')
			rc.segs = append(rc.segs, reconSeg{param: "*"})
		default:
			if lit, ok := regexLiteral(seg); ok {
				b.WriteString(lit)
				rc.segs = append(rc.segs, reconSeg{literal: lit})
			} else {
				key := fmt.Sprintf("%s__%d", v.name, syn)
				syn++
				b.WriteString("{" + key + ":" + seg + "}")
				rc.segs = append(rc.segs, reconSeg{param: key})
			}
		}
	}
	return rc, b.String(), true
}

// fallbackPattern converts a static prefix into a chi catch-all pattern.
func fallbackPattern(prefix string) string {
	if prefix == "" {
		return "/*"
	}
	return prefix + "*"
}

// entryPathMatch reports whether path matches the entry, ignoring method and
// header constraints.
func entryPathMatch(e *routeEntry, path string) bool {
	if e.kind == routeKindPrefix {
		return strings.HasPrefix(path, e.pattern)
	}
	if e.matchRE == nil {
		return false
	}
	return e.matchRE.MatchString(path)
}

// entryMatch reports whether the entry matches method/path (and headers for
// header routes), mirroring mux route matching.
func entryMatch(e *routeEntry, req *http.Request, path string) bool {
	if e.method != "" && e.method != req.Method {
		return false
	}
	if e.kind == routeKindHeader {
		return req.Header.Get(e.header[0]) == e.header[1]
	}
	return entryPathMatch(e, path)
}

// wrapRecon wraps h so that mux-style path variables are reconstructed from
// chi URL params before the handler runs.
func wrapRecon(h http.Handler, e *routeEntry) http.Handler {
	if len(e.recon) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			reconstruct(rctx, e.recon)
		}
		h.ServeHTTP(w, r)
	})
}

// reconstruct rewrites chi URL params applying the reconstruction rules:
// synthetic params are joined back into the original mux-style variable.
func reconstruct(rctx *chi.Context, recon []varRecon) {
	vals := make(map[string]string, len(rctx.URLParams.Keys))
	for i, k := range rctx.URLParams.Keys {
		if i < len(rctx.URLParams.Values) {
			vals[k] = rctx.URLParams.Values[i]
		}
	}
	consumed := make(map[string]struct{})
	rebuilt := make(map[string]string, len(recon))
	for _, rc := range recon {
		var sb strings.Builder
		for i, seg := range rc.segs {
			if i > 0 {
				sb.WriteByte('/')
			}
			if seg.param != "" {
				sb.WriteString(vals[seg.param])
				consumed[seg.param] = struct{}{}
			} else {
				sb.WriteString(seg.literal)
			}
		}
		rebuilt[rc.name] = sb.String()
	}
	keys := make([]string, 0, len(vals))
	values := make([]string, 0, len(vals))
	for i, k := range rctx.URLParams.Keys {
		if _, ok := consumed[k]; ok {
			continue
		}
		if i < len(rctx.URLParams.Values) {
			keys = append(keys, k)
			values = append(values, rctx.URLParams.Values[i])
		}
	}
	for _, rc := range recon {
		keys = append(keys, rc.name)
		values = append(values, rebuilt[rc.name])
	}
	rctx.URLParams.Keys = keys
	rctx.URLParams.Values = values
}

// routeVars returns the mux-style path variables of the request.
func routeVars(r *http.Request) map[string]string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return nil
	}
	vars := make(map[string]string, len(rctx.URLParams.Keys))
	for i, k := range rctx.URLParams.Keys {
		// mux never reports the catch-all "*" param nor variables for
		// prefix routes.
		if k == "*" || i >= len(rctx.URLParams.Values) {
			continue
		}
		vars[k] = rctx.URLParams.Values[i]
	}
	return vars
}

// setPathTemplate overrides the transport operation/path template for
// requests served by the fallback and header dispatchers.
func setPathTemplate(r *http.Request, template string) {
	if tr, ok := transport.FromServerContext(r.Context()); ok {
		if tr, ok := tr.(*Transport); ok {
			tr.operation = template
			tr.pathTemplate = template
		}
	}
}

// strictSlashRedirect implements mux's StrictSlash semantics: if no route
// matches the request path but a route matches the path with a trailing slash
// added or removed, redirect (301) to the canonical path.
func (s *Server) strictSlashRedirect(w http.ResponseWriter, req *http.Request) bool {
	path := req.URL.Path
	if path == "/" {
		return false
	}
	toggled := path + "/"
	if strings.HasSuffix(path, "/") {
		toggled = strings.TrimSuffix(path, "/")
	}
	for _, e := range s.routes.snapshot() {
		if entryMatch(e, req, path) {
			return false
		}
		if entryMatch(e, req, toggled) {
			u := &url.URL{Path: toggled, RawQuery: req.URL.RawQuery}
			http.Redirect(w, req, u.String(), http.StatusMovedPermanently)
			return true
		}
	}
	return false
}

// serveUnmatched handles requests that reached a dispatcher without matching
// any of its routes: 405 when the path matches another method, 404 otherwise.
func (s *Server) serveUnmatched(w http.ResponseWriter, r *http.Request) {
	if s.routes.methodNotAllowed(r.Method, r.URL.Path) {
		s.root.MethodNotAllowedHandler().ServeHTTP(w, r)
		return
	}
	s.root.NotFoundHandler().ServeHTTP(w, r)
}

// fallbackDispatcher dispatches a chi wildcard match to the first fallback
// entry whose full-path regexp matches, in registration order.
func (s *Server) fallbackDispatcher(g *fallbackGroup) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.routes.mu.RLock()
		entries := append([]*routeEntry(nil), g.entries...)
		s.routes.mu.RUnlock()
		for _, e := range entries {
			if e.method != "" && e.method != r.Method {
				continue
			}
			m := e.matchRE.FindStringSubmatch(r.URL.Path)
			if m == nil {
				continue
			}
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				keys := make([]string, 0, len(e.varNames))
				values := make([]string, 0, len(e.varNames))
				for i, name := range e.varNames {
					if i+1 < len(m) {
						keys = append(keys, name)
						values = append(values, m[i+1])
					}
				}
				rctx.URLParams.Keys = keys
				rctx.URLParams.Values = values
			}
			setPathTemplate(r, e.pattern)
			e.handler.ServeHTTP(w, r)
			return
		}
		s.serveUnmatched(w, r)
	})
}

// headerDispatcher dispatches requests to the first header route whose
// key/value matches, in registration order.
func (s *Server) headerDispatcher() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, e := range s.routes.headers() {
			if r.Header.Get(e.header[0]) == e.header[1] {
				// mux GetPathTemplate fails for header-only routes.
				setPathTemplate(r, "")
				e.handler.ServeHTTP(w, r)
				return
			}
		}
		s.serveUnmatched(w, r)
	})
}

// registerRoute registers a mux-style route on the chi router.
// method may be "" to register for all methods.
func (s *Server) registerRoute(method, pattern string, h http.Handler) {
	e := &routeEntry{method: method, pattern: pattern, handler: h}
	translatePattern(e)
	if e.kind == routeKindFallback {
		g, created := s.routes.addFallback(e)
		if created {
			s.router.Handle(e.chiPat, s.fallbackDispatcher(g))
		}
		return
	}
	s.routes.add(e)
	h = wrapRecon(h, e)
	if method == "" {
		s.router.Handle(e.chiPat, h)
		return
	}
	if !standardMethods[method] {
		chi.RegisterMethod(method)
	}
	s.router.Method(method, e.chiPat, h)
}

// registerPrefix registers a path-prefix route (mux PathPrefix equivalent).
func (s *Server) registerPrefix(prefix string, h http.Handler) {
	e := &routeEntry{
		pattern: prefix,
		chiPat:  fallbackPattern(prefix),
		kind:    routeKindPrefix,
		handler: h,
	}
	s.routes.add(e)
	s.router.Handle(e.chiPat, h)
}

// registerHeader registers a header-matching route (mux Headers equivalent).
func (s *Server) registerHeader(key, val string, h http.Handler) {
	e := &routeEntry{
		pattern: "",
		kind:    routeKindHeader,
		header:  [2]string{key, val},
		handler: h,
	}
	s.routes.add(e)
	s.routes.mu.Lock()
	mounted := s.routes.headerMounted
	s.routes.headerMounted = true
	s.routes.mu.Unlock()
	if !mounted {
		s.router.Handle("/*", s.headerDispatcher())
	}
}

var standardMethods = map[string]bool{
	http.MethodConnect: true,
	http.MethodDelete:  true,
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodPatch:   true,
	http.MethodPost:    true,
	http.MethodPut:     true,
	http.MethodTrace:   true,
	"QUERY":            true,
}
