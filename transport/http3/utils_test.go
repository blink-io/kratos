package http3

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"

	"github.com/go-kratos/kratos/v2/testdata"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

type bufferedWriteCloser struct {
	*bufio.Writer
	io.Closer
}

// NewBufferedWriteCloser creates an io.WriteCloser from a bufio.Writer and an io.Closer
func NewBufferedWriteCloser(writer *bufio.Writer, closer io.Closer) io.WriteCloser {
	return &bufferedWriteCloser{
		Writer: writer,
		Closer: closer,
	}
}

func (h bufferedWriteCloser) Close() error {
	if err := h.Writer.Flush(); err != nil {
		return err
	}
	return h.Closer.Close()
}

func generateTLSConfig() *tls.Config {
	return testdata.GetTLSConfig()
}

var h3c = http3Client()

func createClientTLSConfig() *tls.Config {
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Fatal(err)
	}
	testdata.AddRootCA(pool)

	tlsConf := &tls.Config{
		RootCAs:            pool,
		InsecureSkipVerify: true,
		//KeyLogWriter:       keyLog,
		MinVersion: tls.VersionTLS13,
	}
	return tlsConf
}

func http3Client() *http.Client {
	tlsConf := createClientTLSConfig()
	qconf := new(quic.Config)
	//qconf.Tracer = func(ctx context.Context, p logging.Perspective, connID quic.ConnectionID) *logging.ConnectionTracer {
	//	filename := fmt.Sprintf("client_%x.qlog", connID)
	//	f, err := os.Create(filename)
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//	log.Printf("Creating qlog file %s.\n", filename)
	//	return qlog.NewConnectionTracer(NewBufferedWriteCloser(bufio.NewWriter(f), f), p, connID)
	//}
	roundTripper := &http3.RoundTripper{
		TLSClientConfig: tlsConf,
		QuicConfig:      qconf,
	}
	//defer roundTripper.Close()
	hclient := &http.Client{
		Transport: roundTripper,
	}
	return hclient
}
