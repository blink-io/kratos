package http3

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// generateTestTLSConfig returns a self-signed TLS config valid for 127.0.0.1
// and ::1. The certificate is generated once per process and cached so the
// test is fast and deterministic.
var (
	testTLSOnce sync.Once
	testTLSConf *tls.Config
)

func generateTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	testTLSOnce.Do(func() {
		priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}

		template := x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: "127.0.0.1"},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
			BasicConstraintsValid: true,
			IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
			DNSNames:              []string{"localhost"},
		}

		der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
		if err != nil {
			t.Fatal(err)
		}

		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		keyDER, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			t.Fatal(err)
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			t.Fatal(err)
		}
		testTLSConf = &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		}
	})
	return testTLSConf.Clone()
}

func TestServer_NewServerDefaults(t *testing.T) {
	srv := NewServer()
	if srv.Server == nil {
		t.Fatal("Server.http3.Server is nil")
	}
	if srv.inner == nil {
		t.Fatal("Server.inner is nil")
	}
	if srv.Server.Handler == nil {
		t.Fatal("Server.Handler is nil")
	}
}

func TestServer_EndpointRequiresTLS(t *testing.T) {
	srv := NewServer(Address("127.0.0.1:0"))
	_, err := srv.Endpoint()
	if err == nil {
		t.Fatal("expected error when TLSConfig is not provided")
	}
}

func TestServer_EndpointWithTLS(t *testing.T) {
	tlsConf := generateTestTLSConfig(t)
	srv := NewServer(
		Address("127.0.0.1:0"),
		TLSConfig(tlsConf),
	)
	ep, err := srv.Endpoint()
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if ep == nil {
		t.Fatal("nil endpoint")
	}
	if ep.Scheme != "https" {
		t.Fatalf("scheme = %q, want %q", ep.Scheme, "https")
	}
	// Stop the listener so the test cleans up.
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestServer_HandleFunc(t *testing.T) {
	srv := NewServer()
	srv.HandleFunc("/hi", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	})
	// Drive the handler chain directly (no QUIC).
	req, err := http.NewRequest(http.MethodGet, "https://example.com/hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	rw := &testResponseWriter{header: http.Header{}}
	srv.ServeHTTP(rw, req)
	if rw.code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.code)
	}
	if got := string(rw.body); got != "hi" {
		t.Fatalf("body = %q, want %q", got, "hi")
	}
}

func TestServer_Route(t *testing.T) {
	srv := NewServer()
	srv.Route("/v1").GET("/ping", func(c Context) error {
		return c.Result(http.StatusOK, map[string]string{"msg": "pong"})
	})
	req, err := http.NewRequest(http.MethodGet, "https://example.com/v1/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	rw := &testResponseWriter{header: http.Header{}}
	srv.ServeHTTP(rw, req)
	if rw.code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.code)
	}
	if got := string(rw.body); got != "{\"msg\":\"pong\"}" {
		t.Fatalf("body = %q, want %q", got, `{"msg":"pong"}`)
	}
}

type testResponseWriter struct {
	header http.Header
	body   []byte
	code   int
}

func (w *testResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *testResponseWriter) Write(b []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}
