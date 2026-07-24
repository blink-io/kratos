package http

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-kratos/kratos/v3/transport"
)

func TestTranslatePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		chiPat  string
		kind    routeKind
		recon   []varRecon
	}{
		{
			name:    "static",
			pattern: "/index",
			chiPat:  "/index",
			kind:    routeKindNormal,
		},
		{
			name:    "single var",
			pattern: "/users/{name}",
			chiPat:  "/users/{name}",
			kind:    routeKindNormal,
		},
		{
			name:    "single segment regex",
			pattern: "/index/{id:[0-9]+}",
			chiPat:  "/index/{id:[0-9]+}",
			kind:    routeKindNormal,
		},
		{
			name:    "regex quantifier with braces",
			pattern: "/index/{id:[0-9]{1,2}}",
			chiPat:  "/index/{id:[0-9]{1,2}}",
			kind:    routeKindNormal,
		},
		{
			name:    "single segment negated class",
			pattern: "/test/{namespace:[^/]+}",
			chiPat:  "/test/{namespace:[^/]+}",
			kind:    routeKindNormal,
		},
		{
			name:    "multi segment with literal prefix",
			pattern: "/test/{name:messages/[^/]+}",
			chiPat:  "/test/messages/{name__0:[^/]+}",
			kind:    routeKindNormal,
			recon: []varRecon{
				{name: "name", segs: []reconSeg{{literal: "messages"}, {param: "name__0"}}},
			},
		},
		{
			name:    "multi segment with literal infix",
			pattern: "/v1/{name:publishers/[^/]+/books/[^/]+}",
			chiPat:  "/v1/publishers/{name__0:[^/]+}/books/{name__1:[^/]+}",
			kind:    routeKindNormal,
			recon: []varRecon{
				{name: "name", segs: []reconSeg{{literal: "publishers"}, {param: "name__0"}, {literal: "books"}, {param: "name__1"}}},
			},
		},
		{
			name:    "trailing multi segment wildcard",
			pattern: "/v1/{name:shelves/.*}",
			chiPat:  "/v1/shelves/*",
			kind:    routeKindNormal,
			recon: []varRecon{
				{name: "name", segs: []reconSeg{{literal: "shelves"}, {param: "*"}}},
			},
		},
		{
			name:    "trailing catch-all regex",
			pattern: "/files/{path:.*}",
			chiPat:  "/files/*",
			kind:    routeKindNormal,
			recon: []varRecon{
				{name: "path", segs: []reconSeg{{param: "*"}}},
			},
		},
		{
			name:    "middle multi segment wildcard falls back",
			pattern: "/a/{x:.*}/b",
			chiPat:  "/a/*",
			kind:    routeKindFallback,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &routeEntry{pattern: tt.pattern}
			translatePattern(e)
			if e.chiPat != tt.chiPat {
				t.Errorf("chiPat: want %q, got %q", tt.chiPat, e.chiPat)
			}
			if e.kind != tt.kind {
				t.Errorf("kind: want %v, got %v", tt.kind, e.kind)
			}
			if !reflect.DeepEqual(e.recon, tt.recon) {
				t.Errorf("recon: want %+v, got %+v", tt.recon, e.recon)
			}
			if e.matchRE == nil {
				t.Errorf("matchRE is nil")
			}
		})
	}
}

func TestRouteVarsReconstruction(t *testing.T) {
	srv := NewServer()
	r := srv.Route("/")
	r.GET("/test/{message.name:messages/[^/]+}", func(ctx Context) error {
		return ctx.String(http.StatusOK, ctx.Vars().Get("message.name"))
	})
	r.GET("/v1/{name:publishers/[^/]+/books/[^/]+}", func(ctx Context) error {
		return ctx.String(http.StatusOK, ctx.Vars().Get("name"))
	})
	r.GET("/v2/{name:shelves/.*}", func(ctx Context) error {
		return ctx.String(http.StatusOK, ctx.Vars().Get("name"))
	})
	r.GET("/a/{x:.*}/b", func(ctx Context) error {
		return ctx.String(http.StatusOK, ctx.Vars().Get("x"))
	})

	tests := []struct {
		path string
		want string
	}{
		{"/test/messages/123", "messages/123"},
		{"/v1/publishers/p1/books/b1", "publishers/p1/books/b1"},
		{"/v2/shelves/a/b/c", "shelves/a/b/c"},
		{"/a/1/2/b", "1/2"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tt.path, w.Code)
		}
		if w.Body.String() != tt.want {
			t.Errorf("%s: want %q, got %q", tt.path, tt.want, w.Body.String())
		}
	}
}

func TestStrictSlashRedirect(t *testing.T) {
	srv := NewServer()
	srv.HandleFunc("/index", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv.HandleFunc("/trail/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		path     string
		wantCode int
		wantLoc  string
	}{
		{"/index", http.StatusOK, ""},
		{"/index/", http.StatusMovedPermanently, "/index"},
		{"/trail/", http.StatusOK, ""},
		{"/trail", http.StatusMovedPermanently, "/trail/"},
		{"/not-exist/", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != tt.wantCode {
			t.Errorf("%s: want code %d, got %d", tt.path, tt.wantCode, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != tt.wantLoc {
			t.Errorf("%s: want location %q, got %q", tt.path, tt.wantLoc, loc)
		}
	}
}

func TestStrictSlashDisabled(t *testing.T) {
	srv := NewServer(StrictSlash(false))
	srv.HandleFunc("/index", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/index/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandlePrefixBoundary(t *testing.T) {
	h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	srv := NewServer()
	srv.HandlePrefix("/test/prefix", http.HandlerFunc(h))

	for _, path := range []string{"/test/prefix", "/test/prefix/", "/test/prefix/123", "/test/prefixfoo"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, w.Code)
		}
	}
}

func TestMethodNotAllowedDefault(t *testing.T) {
	srv := NewServer()
	srv.Route("/").GET("/get-only", func(Context) error { return nil })

	// the default MethodNotAllowedHandler is http.DefaultServeMux,
	// which responds 404 like mux did.
	req := httptest.NewRequest(http.MethodPost, "/get-only", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestDefaultRequestVarsWithoutRouteContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/index", nil)
	var v struct{}
	if err := DefaultRequestVars(req, &v); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestWalkRouteWithPrefix(t *testing.T) {
	srv := NewServer()
	srv.Route("/").GET("/index", func(Context) error { return nil })
	var routes []RouteInfo
	if err := srv.WalkRoute(func(r RouteInfo) error {
		routes = append(routes, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0] != (RouteInfo{Method: http.MethodGet, Path: "/index"}) {
		t.Errorf("unexpected routes: %+v", routes)
	}

	srv.HandlePrefix("/prefix", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err := srv.WalkRoute(func(RouteInfo) error { return nil }); err == nil {
		t.Error("expected error for prefix route, got nil")
	}
}

func TestServerRoutePathTemplate(t *testing.T) {
	srv := NewServer()
	var got string
	srv.Route("/").GET("/users/{name}", func(ctx Context) error {
		tr, ok := transport.FromServerContext(ctx)
		if !ok {
			t.Error("no transport in server context")
			return nil
		}
		got = tr.(Transporter).PathTemplate()
		return ctx.String(http.StatusOK, "")
	})
	req := httptest.NewRequest(http.MethodGet, "/users/foo", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if got != "/users/{name}" {
		t.Errorf("want /users/{name}, got %q", got)
	}
}
