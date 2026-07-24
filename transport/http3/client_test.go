package http3

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestClient_NewClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := NewClient(ctx, WithEndpoint("https://127.0.0.1:443"))
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestClient_DefaultCodecFunctions(t *testing.T) {
	type msg struct {
		Name string `json:"name"`
	}
	body, err := DefaultRequestEncoder(context.Background(), "application/json", msg{Name: "kratos"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(body) != `{"name":"kratos"}` {
		t.Fatalf("encoded body = %q", string(body))
	}
	res := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"name":"kratos"}`)),
	}
	var got msg
	if err := DefaultResponseDecoder(context.Background(), res, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "kratos" {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestClient_ParseTarget(t *testing.T) {
	tests := []struct {
		endpoint  string
		scheme    string
		authority string
	}{
		{"https://example.com:443", "https", "example.com:443"},
		{"example.com:443", "https", "example.com:443"},
		{"https://example.com/path", "https", "example.com"},
	}
	for _, tt := range tests {
		target, err := parseTarget(tt.endpoint)
		if err != nil {
			t.Fatalf("parseTarget(%q): %v", tt.endpoint, err)
		}
		if target.Scheme != tt.scheme {
			t.Errorf("scheme = %q, want %q", target.Scheme, tt.scheme)
		}
		if target.Authority != tt.authority {
			t.Errorf("authority = %q, want %q", target.Authority, tt.authority)
		}
	}
}
