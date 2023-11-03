package tests

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/go-kratos/kratos/v2/testdata"
	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"
)

func TestQuic_Server1(t *testing.T) {
	caFile, keyFile := testdata.GetCertificatePaths()
	hdlr := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("http3"))
	})
	err := http3.ListenAndServeQUIC(":9999", caFile, keyFile, hdlr)
	require.NoError(t, err)
}

func TestQuic_Client1(t *testing.T) {
	tlsConf := testdata.CreateClientTLSConfig()
	hc := &http.Client{Transport: &http3.RoundTripper{
		TLSClientConfig: tlsConf,
		QuicConfig:      nil,
	}}
	res, err := hc.Get("https://localhost:9999")
	bd := res.Body
	defer bd.Close()
	data, err := io.ReadAll(bd)
	require.NoError(t, err)

	fmt.Println("res body: ", string(data))

	require.NoError(t, err)
	require.NotNil(t, res)
}
