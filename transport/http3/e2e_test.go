package http3

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"testing"
	"time"
)

// TestClientServerEndToEnd exercises the full HTTP/3 stack: it brings up an
// http3.Server, dials it from an http3 client, and checks the response body.
//
// This test is skipped in -short mode because it relies on a UDP loopback
// socket and a real QUIC handshake, which can be flaky under some CI
// environments.
func TestClientServerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping http3 end-to-end test in -short mode")
	}

	tlsConf := generateTestTLSConfig(t)
	srv := NewServer(
		Address("127.0.0.1:0"),
		TLSConfig(tlsConf),
		Timeout(2*time.Second),
	)
	srv.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Start(ctx)
	}()

	// Resolve the listening address so the client knows where to dial.
	deadline := time.Now().Add(2 * time.Second)
	for srv.lis == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.lis == nil {
		t.Fatal("server did not bind in time")
	}
	addr := srv.lis.LocalAddr().String()

	// Build a TLS config that trusts the test cert. InsecureSkipVerify is
	// acceptable here because the server is bound to 127.0.0.1 and the
	// certificate is generated in-process for the test.
	pool := x509PoolFromConfig(tlsConf)
	if len(pool.Subjects()) == 0 {
		t.Fatal("empty cert pool, cannot verify server")
	}

	client, err := NewClient(ctx,
		WithEndpoint("https://"+addr),
		WithTLSConfig(&tls.Config{
			RootCAs:    pool,
			ServerName: "127.0.0.1",
			MinVersion: tls.VersionTLS13,
		}),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+addr+"/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	buf := make([]byte, 4)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != "pong" {
		t.Fatalf("body = %q, want %q", got, "pong")
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Logf("stop: %v", err)
	}
	// Drain the serve error.
	select {
	case <-serveErr:
	case <-time.After(2 * time.Second):
	}
}

// x509PoolFromConfig returns a *x509.CertPool containing the leaf certificate
// of the first entry in tlsConf.Certificates. This is only useful for the
// in-process test where the same self-signed cert is shared by client and
// server.
func x509PoolFromConfig(tlsConf *tls.Config) *x509.CertPool {
	pool := x509.NewCertPool()
	if len(tlsConf.Certificates) == 0 {
		return pool
	}
	for _, der := range tlsConf.Certificates[0].Certificate {
		// tls.Certificate.Certificate is a slice of DER-encoded certs, so
		// parse directly without going through PEM.
		if cert, err := x509.ParseCertificate(der); err == nil {
			pool.AddCert(cert)
		}
	}
	return pool
}
